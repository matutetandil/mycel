#!/bin/bash
# <UDF name="target_ip" label="Target IP" />
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

# Install k6
curl -fsSL https://dl.k6.io/key.gpg | gpg --dearmor -o /usr/share/keyrings/k6-archive-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" > /etc/apt/sources.list.d/k6.list
apt-get update && apt-get install -y k6 jq

# Install wrk (for raw max-RPS measurement)
apt-get install -y wrk

# Save target IP
mkdir -p /opt/benchmark
echo "$LINODE_TARGET_IP" > /opt/benchmark/target_ip

# Copy k6 script
cat > /opt/benchmark/loadtest.js << 'K6SCRIPT'
LOADTEST_PLACEHOLDER
K6SCRIPT

# Copy run script
cat > /opt/benchmark/run.sh << 'RUNSCRIPT'
#!/bin/bash
TARGET_IP=$(cat /opt/benchmark/target_ip)
exec /opt/benchmark/run-core.sh remote "$TARGET_IP"
RUNSCRIPT
chmod +x /opt/benchmark/run.sh

echo "Attacker ready. Target: $LINODE_TARGET_IP"
