// Command sora runs the Service Operations Remediation Agent.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	redisv9 "github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"

	"github.com/teochenglim/sora/internal/cache"
	"github.com/teochenglim/sora/internal/circuit"
	"github.com/teochenglim/sora/internal/classifier"
	"github.com/teochenglim/sora/internal/config"
	"github.com/teochenglim/sora/internal/dedup"
	"github.com/teochenglim/sora/internal/incident"
	"github.com/teochenglim/sora/internal/notifier"
	"github.com/teochenglim/sora/internal/remediator"
	"github.com/teochenglim/sora/internal/tools"
	"github.com/teochenglim/sora/internal/webhook"
	"github.com/teochenglim/sora/pkg/logger"
)

var buildVersion = "dev"

func main() {
	mode := flag.String("mode", "", "classifier | remediation | full | notify-only | demo (overrides config.yaml's mode if set)")
	configPath := flag.String("config", "", "path to config.yaml")
	logLevel := flag.String("log-level", "info", "logrus level")
	flag.Parse()

	webhook.Version = buildVersion
	log := logger.New(*logLevel)

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.WithError(err).Fatal("loading config")
	}
	if *mode != "" {
		cfg.Mode = *mode
	}
	if cfg.Mode == "" {
		cfg.Mode = "full"
	}

	if err := run(cfg, log); err != nil {
		log.WithError(err).Fatal("sora exited with error")
	}
}

func run(cfg *config.Config, log *logrus.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	demo := cfg.Mode == "demo"

	rulesStore, err := config.NewRulesStore(cfg.RulesPath)
	if err != nil {
		return fmt.Errorf("loading rules: %w", err)
	}
	watchSIGHUP(rulesStore, log)

	deduper, store, redisClient := buildPersistence(ctx, cfg, demo, log)
	defer deduper.Close()
	defer store.Close()

	ctxCache := cache.New(15 * time.Minute)
	breaker := circuit.New("ai", 10, 0.5, 60*time.Second)

	var aiClient classifier.AIClassifier
	if !demo && cfg.Mode != "remediation" && cfg.AI.APIKey != "" {
		aiClient = classifier.NewAIClassifier(cfg.AI)
	}
	ruleClassifier := classifier.NewRuleClassifier(rulesStore, cfg.FallbackRules)
	orchestrator := classifier.New(aiClient, ruleClassifier, breaker, ctxCache, cfg.BusinessMappings)

	notifiers := buildNotifiers(cfg, demo)

	var engine *remediator.Engine
	var learning *remediator.LearningStore
	if cfg.Mode == "full" || cfg.Mode == "remediation" || demo {
		engine, learning, err = buildRemediation(cfg, rulesStore, store, notifiers, redisClient, demo, log)
		if err != nil {
			return fmt.Errorf("building remediation engine: %w", err)
		}
		if learning != nil {
			defer learning.Close()
			go runLearningTicker(ctx, learning, cfg.RulesPath, log)
		}
	}

	h := &webhook.Handler{
		Mode: cfg.Mode, Cfg: cfg, Classifier: orchestrator, Deduper: deduper,
		Engine: engine, Notifiers: notifiers, Store: store, Learning: learning, Log: log,
		StartedAt: time.Now(),
	}
	mux := http.NewServeMux()
	h.Routes(mux)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	if demo {
		go runDemoGenerator(ctx, h, log)
	}

	go func() {
		log.WithField("port", cfg.Server.Port).WithField("mode", cfg.Mode).Info("sora listening")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.WithError(err).Error("http server error")
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func buildPersistence(ctx context.Context, cfg *config.Config, demo bool, log *logrus.Logger) (dedup.Deduper, incident.Store, *redisv9.Client) {
	window := time.Duration(cfg.Dedup.WindowSeconds) * time.Second
	if demo || cfg.Dedup.RedisAddr == "" {
		log.Info("using in-memory dedup and incident store")
		return dedup.NewMemoryDeduper(window), incident.NewMemoryStore(), nil
	}

	d, err := dedup.NewRedisDeduper(ctx, cfg.Dedup.RedisAddr, cfg.Dedup.RedisPassword, window)
	if err != nil {
		log.WithError(err).Warn("redis unavailable, falling back to in-memory dedup")
		return dedup.NewMemoryDeduper(window), incident.NewMemoryStore(), nil
	}
	s, err := incident.NewRedisStore(ctx, cfg.Dedup.RedisAddr, cfg.Dedup.RedisPassword)
	if err != nil {
		log.WithError(err).Warn("redis unavailable, falling back to in-memory incident store")
		return d, incident.NewMemoryStore(), nil
	}
	client := redisv9.NewClient(&redisv9.Options{Addr: cfg.Dedup.RedisAddr, Password: cfg.Dedup.RedisPassword})
	return d, s, client
}

func buildNotifiers(cfg *config.Config, demo bool) []notifier.Notifier {
	if demo {
		return nil
	}
	wh := notifier.WorkHours{Start: cfg.WorkHours.Start, End: cfg.WorkHours.End, Timezone: cfg.WorkHours.Timezone, Days: cfg.WorkHours.Days}
	var notifiers []notifier.Notifier
	if cfg.Notifications.Slack.WebhookURL != "" {
		notifiers = append(notifiers, notifier.NewSlackNotifier(cfg.Notifications.Slack.WebhookURL, cfg.BusinessOwners, wh))
	}
	if cfg.Notifications.Telegram.BotToken != "" {
		notifiers = append(notifiers, notifier.NewTelegramNotifier(cfg.Notifications.Telegram.BotToken, cfg.Notifications.Telegram.DefaultChatID, cfg.BusinessOwners, wh))
	}
	return notifiers
}

func buildRemediation(cfg *config.Config, rulesStore *config.RulesStore, store incident.Store, notifiers []notifier.Notifier, redisClient *redisv9.Client, demo bool, log *logrus.Logger) (*remediator.Engine, *remediator.LearningStore, error) {
	var registry *tools.Registry
	var rateLimiter remediator.RateLimiter
	if demo {
		registry = tools.NewDemoRegistry()
		rateLimiter = remediator.NewMemoryRateLimiter()
	} else {
		cs, err := tools.NewClientset(cfg.Remediation.Kubeconfig)
		if err != nil {
			return nil, nil, fmt.Errorf("building k8s clientset: %w", err)
		}
		registry = tools.NewRegistry(
			&tools.QueryPodStatusTool{Clientset: cs},
			&tools.QueryLogsTool{Clientset: cs},
			&tools.RestartServiceTool{Clientset: cs},
		)
		if redisClient != nil {
			rateLimiter = remediator.NewRedisRateLimiter(redisClient)
		} else {
			rateLimiter = remediator.NewMemoryRateLimiter()
		}
	}

	executor := remediator.NewExecutor(registry, cfg.Remediation.MaxTier2ToolCalls, cfg.Remediation.DryRun)
	tier1 := remediator.NewTier1(rulesStore, executor, rateLimiter, cfg.Remediation)

	var tier2 *remediator.Tier2
	if !demo && cfg.AI.APIKey != "" {
		baseURL := cfg.AI.BaseURL
		planner := remediator.NewHTTPPlanner(cfg.AI, baseURL)
		tier2 = remediator.NewTier2(planner, executor, cfg.Remediation.MaxTier2ToolCalls, cfg.Remediation.ToolTimeout)
	}

	tier3 := remediator.NewTier3(notifiers, store, cfg.Remediation.ApprovalTimeout, cfg.Notifications.PagerDuty.IntegrationKey)
	verifier := remediator.NewVerifier(executor, cfg.Remediation.VerificationWait)

	var learning *remediator.LearningStore
	if !demo {
		l, err := remediator.NewLearningStore("sora-learning.db")
		if err != nil {
			log.WithError(err).Warn("learning store unavailable, continuing without pattern learning")
		} else {
			learning = l
		}
	}

	engine := remediator.NewEngine(tier1, tier2, tier3, verifier, store, learning)
	return engine, learning, nil
}

func runLearningTicker(ctx context.Context, learning *remediator.LearningStore, rulesPath string, log *logrus.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := learning.PromoteEligible(ctx, rulesPath)
			if err != nil {
				log.WithError(err).Error("pattern promotion failed")
				continue
			}
			if n > 0 {
				log.WithField("promoted", n).Info("promoted learned patterns to tier1 rules")
			}
		}
	}
}

func runDemoGenerator(ctx context.Context, h *webhook.Handler, log *logrus.Logger) {
	gen := tools.NewDemoAlertGenerator()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a := gen.Next()
			log.WithField("alert", a.String()).Info("demo mode: firing sample alert")
			h.IngestDemoAlert(ctx, a)
		}
	}
}

func watchSIGHUP(rulesStore *config.RulesStore, log *logrus.Logger) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	go func() {
		for range ch {
			if err := rulesStore.Reload(); err != nil {
				log.WithError(err).Error("failed to reload rules.yaml on SIGHUP")
				continue
			}
			log.Info("reloaded rules.yaml")
		}
	}()
}
