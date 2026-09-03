#!/bin/bash
ORIGINAL_IDLE=$(gsettings get org.gnome.settings-daemon.plugins.power sleep-inactive-ac-type 2>/dev/null || echo "nothing")
gsettings set org.gnome.settings-daemon.plugins.power sleep-inactive-ac-type nothing 2>/dev/null

cd /home/tlavocat/dev/operator-certmanager
LOGFILE="/home/tlavocat/dev/operator-certmanager/e2e_$(date +%Y%m%d_%H%M%S).log"
echo "=== E2E log: $LOGFILE ==="
USE_EXISTING_CLUSTER=true RECONCILE_RESYNC_PERIOD=10s go test -v ./controllers -run TestAPIs -ginkgo.label-filter='!do' -timeout 120m 2>&1 | tee "$LOGFILE"
EXIT=$?

gsettings set org.gnome.settings-daemon.plugins.power sleep-inactive-ac-type "$ORIGINAL_IDLE" 2>/dev/null
exit $EXIT
