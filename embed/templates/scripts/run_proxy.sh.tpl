#!/bin/bash
set -e

# WARNING: This file was auto-generated. Do not edit!
#          All your edit might be overwritten!
DEPLOY_DIR={{.DeployDir}}

cd "${DEPLOY_DIR}" || exit 1

{{- define "PDList"}}
  {{- range $idx, $pd := .}}
    {{- if eq $idx 0}}
      {{- $pd.IP}}:{{$pd.ClientPort}}
    {{- else -}}
      ,{{$pd.IP}}:{{$pd.ClientPort}}
    {{- end}}
  {{- end}}
{{- end}}

{{- if .NumaNode}}
exec numactl --cpunodebind={{.NumaNode}} --membind={{.NumaNode}} env GODEBUG=madvdontneed=1 bin/tikv-proxy \
{{- else}}
exec env GODEBUG=madvdontneed=1 bin/tikv-proxy \
{{- end}}
    --name="{{.Name}}" \
    --client-addr="{{.ListenHost}}:{{.Port}}" \
    --pd-addrs="{{template "PDList" .Endpoints}}" \
    --config=conf/proxy.toml \
    --log-file="{{.LogDir}}/proxy.log" 2>> "{{.LogDir}}/proxy_stderr.log"
