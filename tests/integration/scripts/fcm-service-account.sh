#!/usr/bin/env bash
# Print a throwaway Firebase service account for the push connector.
#
# Firebase's v1 API — the only one Google still answers — authenticates with a
# service account rather than a shared key, and the connector signs a JWT with
# its private key at startup. So the suite needs a real key pair.
#
# Generated per run rather than committed: nothing key-shaped belongs in the
# repository, and a file that looks like a private key is the kind of thing
# push protection stops at the worst moment.
#
# token_uri points at the mock, which answers with an access token.
set -euo pipefail

FCM_KEY=$(openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 2>/dev/null)

FCM_KEY="$FCM_KEY" python3 -c '
import json, os
print(json.dumps({
    "type": "service_account",
    "project_id": "mycel-integration",
    "private_key": os.environ["FCM_KEY"],
    "client_email": "push@mycel-integration.iam.gserviceaccount.com",
    "token_uri": "http://mock:8888/token",
}))'
