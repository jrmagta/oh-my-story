{{- define "oms.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "oms.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "oms.labels" -}}
{{ include "oms.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{- define "oms.image" -}}
{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}
{{- end -}}

{{- define "oms.secretName" -}}
{{ .Values.existingSecret | default (include "oms.fullname" .) }}
{{- end -}}

{{- define "oms.cnpgCluster" -}}
{{ include "oms.fullname" . }}-db
{{- end -}}

{{/* Data for the chart-made Secret; empty when existingSecret is set. */}}
{{- define "oms.secretData" -}}
{{- if not .Values.existingSecret -}}
{{- if not .Values.cloudnativepg.enabled }}
database-url: {{ required "database.url is required unless existingSecret is set or cloudnativepg.enabled" .Values.database.url | b64enc }}
{{- end }}
{{- if .Values.oidc.issuer }}
oidc-client-secret: {{ required "oidc.clientSecret is required when oidc.issuer is set" .Values.oidc.clientSecret | b64enc }}
{{- end }}
{{- if .Values.smtp.password }}
smtp-password: {{ .Values.smtp.password | b64enc }}
{{- end }}
{{- end -}}
{{- end -}}

{{/* Container env shared by the app and the migration Job: the binary validates the whole config for every command. */}}
{{- define "oms.env" -}}
- name: OMS_HTTP_ADDR
  value: ":8080"
- name: OMS_BASE_URL
  value: {{ required "baseURL is required" .Values.baseURL | quote }}
- name: OMS_DATABASE_URL
  valueFrom:
    secretKeyRef:
{{- if .Values.cloudnativepg.enabled }}
      name: {{ include "oms.cnpgCluster" . }}-app
      key: uri
{{- else }}
      name: {{ include "oms.secretName" . }}
      key: database-url
{{- end }}
{{- with .Values.smtp }}
{{- if .addr }}
- name: OMS_SMTP_ADDR
  value: {{ .addr | quote }}
{{- end }}
{{- if .username }}
- name: OMS_SMTP_USERNAME
  value: {{ .username | quote }}
{{- end }}
{{- if .from }}
- name: OMS_MAIL_FROM
  value: {{ .from | quote }}
{{- end }}
{{- end }}
{{- if or .Values.smtp.password .Values.existingSecret }}
- name: OMS_SMTP_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "oms.secretName" . }}
      key: smtp-password
      optional: true
{{- end }}
{{- with .Values.oidc }}
{{- if .issuer }}
- name: OMS_OIDC_ISSUER
  value: {{ .issuer | quote }}
{{- if .discoveryURL }}
- name: OMS_OIDC_DISCOVERY_URL
  value: {{ .discoveryURL | quote }}
{{- end }}
- name: OMS_OIDC_CLIENT_ID
  value: {{ required "oidc.clientID is required when oidc.issuer is set" .clientID | quote }}
- name: OMS_OIDC_CLIENT_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ include "oms.secretName" $ }}
      key: oidc-client-secret
{{- if .name }}
- name: OMS_OIDC_NAME
  value: {{ .name | quote }}
{{- end }}
{{- end }}
{{- end }}
{{- end -}}

{{- define "oms.podSecurityContext" -}}
runAsNonRoot: true
# distroless "nonroot"; kubelet needs the numeric id to check runAsNonRoot.
runAsUser: 65532
runAsGroup: 65532
seccompProfile:
  type: RuntimeDefault
{{- end -}}

{{- define "oms.containerSecurityContext" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities:
  drop: [ALL]
{{- end -}}
