#!/usr/bin/env bash
# Checks a Helm release "oms" in namespace $1 through its Service: the app is
# up, reaches Postgres, and the schema is migrated (GET /api/setup reads users).
set -euo pipefail
ns=$1

kubectl -n "$ns" port-forward svc/oms-oh-my-story 18080:80 >/dev/null &
trap 'kill $!' EXIT
for _ in $(seq 30); do curl -sf localhost:18080/healthz >/dev/null && break; sleep 1; done

health=$(curl -sf localhost:18080/healthz)
setup=$(curl -sf localhost:18080/api/setup)
echo "healthz: $health"
echo "setup:   $setup"
[[ $health == *'"status":"ok"'* ]]
[[ $setup == *'"needsBootstrap":true'* ]]
