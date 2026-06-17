package tools

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// NewClientset builds a Kubernetes clientset, using the in-cluster config
// when kubeconfigPath is empty and SORA is running inside a cluster,
// otherwise loading the given kubeconfig file.
func NewClientset(kubeconfigPath string) (*kubernetes.Clientset, error) {
	var cfg *rest.Config
	var err error
	if kubeconfigPath == "" {
		cfg, err = rest.InClusterConfig()
	} else {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}
	if err != nil {
		return nil, fmt.Errorf("building kubernetes config: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("building kubernetes clientset: %w", err)
	}
	return cs, nil
}

// QueryPodStatusTool reports phase and restart count for a pod.
type QueryPodStatusTool struct {
	Clientset *kubernetes.Clientset
}

func (t *QueryPodStatusTool) Name() string { return ToolQueryPodStatus }

func (t *QueryPodStatusTool) Execute(ctx context.Context, params map[string]string) (Result, error) {
	ns, pod := params["namespace"], params["pod"]
	if ns == "" || pod == "" {
		return Result{}, fmt.Errorf("query_pod_status requires namespace and pod params")
	}
	p, err := t.Clientset.CoreV1().Pods(ns).Get(ctx, pod, metav1.GetOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("getting pod %s/%s: %w", ns, pod, err)
	}
	restarts := 0
	for _, cs := range p.Status.ContainerStatuses {
		restarts += int(cs.RestartCount)
	}
	out := fmt.Sprintf("phase=%s restarts=%d", p.Status.Phase, restarts)
	return Result{Output: out, Success: p.Status.Phase == corev1.PodRunning}, nil
}

// QueryLogsTool fetches recent container logs for a pod.
type QueryLogsTool struct {
	Clientset *kubernetes.Clientset
}

func (t *QueryLogsTool) Name() string { return ToolQueryLogs }

func (t *QueryLogsTool) Execute(ctx context.Context, params map[string]string) (Result, error) {
	ns, pod := params["namespace"], params["pod"]
	if ns == "" || pod == "" {
		return Result{}, fmt.Errorf("query_logs requires namespace and pod params")
	}
	tail := int64(100)
	if v, ok := params["tail_lines"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			tail = n
		}
	}
	opts := &corev1.PodLogOptions{TailLines: &tail}
	if c, ok := params["container"]; ok {
		opts.Container = c
	}
	req := t.Clientset.CoreV1().Pods(ns).GetLogs(pod, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("streaming logs for %s/%s: %w", ns, pod, err)
	}
	defer stream.Close()

	data, err := io.ReadAll(stream)
	if err != nil {
		return Result{}, fmt.Errorf("reading logs for %s/%s: %w", ns, pod, err)
	}
	return Result{Output: string(data), Success: true}, nil
}

// RestartServiceTool performs a rolling restart of a Deployment by
// patching its pod template annotation, equivalent to `kubectl rollout restart`.
type RestartServiceTool struct {
	Clientset *kubernetes.Clientset
}

func (t *RestartServiceTool) Name() string { return ToolRestartService }

func (t *RestartServiceTool) Execute(ctx context.Context, params map[string]string) (Result, error) {
	ns, service := params["namespace"], params["service"]
	if ns == "" || service == "" {
		return Result{}, fmt.Errorf("restart_service requires namespace and service params")
	}
	deployments := t.Clientset.AppsV1().Deployments(ns)
	dep, err := deployments.Get(ctx, service, metav1.GetOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("getting deployment %s/%s: %w", ns, service, err)
	}
	if dep.Spec.Template.Annotations == nil {
		dep.Spec.Template.Annotations = map[string]string{}
	}
	dep.Spec.Template.Annotations["sora.io/restartedAt"] = time.Now().UTC().Format(time.RFC3339)

	if _, err := deployments.Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		return Result{}, fmt.Errorf("updating deployment %s/%s: %w", ns, service, err)
	}
	return Result{Output: fmt.Sprintf("restart triggered for %s/%s", ns, service), Success: true}, nil
}
