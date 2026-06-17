{{- define "sora.name" -}}
sora
{{- end -}}

{{- define "sora.fullname" -}}
{{- .Release.Name -}}-sora
{{- end -}}

{{- define "sora.labels" -}}
app.kubernetes.io/name: {{ include "sora.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "sora.selectorLabels" -}}
app: {{ include "sora.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "sora.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "sora.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "sora.secretName" -}}
{{- if .Values.secrets.existingSecret -}}
{{- .Values.secrets.existingSecret -}}
{{- else -}}
{{- include "sora.fullname" . -}}-secrets
{{- end -}}
{{- end -}}
