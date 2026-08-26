# TLS

How Mycel decides whether to trust the service it is calling.

Three connectors, one endpoint, three answers: verified against a certificate
authority you name, verified against the machine's trust store, and not
verified at all.

## Files

| File | Purpose |
|------|---------|
| `connectors.mycel` | The same HTTPS endpoint, configured three ways |
| `flows.mycel` | One call through each |

## Running

You need something serving HTTPS with a certificate you have. With the
integration stack up, its mock server is one — it signs its own and hands the
certificate out over plain HTTP:

```bash
mkdir -p certs
curl -s http://localhost:8889/ca.pem > certs/ca.pem

export TLS_URL=https://localhost:8443
export TLS_CA_CERT=./certs/ca.pem
mycel start --config ./examples/tls
```

Any HTTPS service will do instead: point `TLS_URL` at it and `TLS_CA_CERT` at
the CA that signed it.

## Try It

**Verified against the CA you named.** `ca_cert` replaces the machine's trust
store for this connector — the certificate has to be signed by that authority
and by no other:

```bash
curl http://localhost:3000/internal
```

**Nothing configured**, which means the machine's trust store. Right for a
public certificate, wrong here: this endpoint signed its own, so nobody has any
reason to trust it and the call is refused.

```bash
curl http://localhost:3000/untrusted
```

```json
{ "error": "request failed: Get \"https://…/requests\": tls: failed to verify certificate: x509: certificate signed by unknown authority" }
```

**Verification off.** It works, and it works against anything — including
whoever is between you and the service you meant to reach:

```bash
curl http://localhost:3000/unverified
```

The service says so at startup, once per connector, in as many words:

```
WARN TLS verification disabled for HTTP connector — never use in production connector=unverified
```

## What the block takes

| Attribute | What it does |
|-----------|--------------|
| `ca_cert` | Path to the CA bundle used to verify the other side. Naming one replaces the system roots for this connector |
| `cert` / `key` | The client certificate this connector presents, for mutual TLS |
| `insecure_skip_verify` | Accept any certificate. Development only |
| `enabled` | `false` turns the block off without deleting it |

## Notes

**A TLS configuration that cannot be built stops the service.** A `ca_cert`
path that cannot be read, or a file that is not a certificate, used to be
discarded — leaving the connector on the system roots while the configuration
said otherwise, which looks like working TLS from the outside. It is refused
at startup now.

**The REST server speaks plain HTTP.** There is no `tls` block on it and no
certificate to configure: terminate TLS in front of Mycel, at an ingress, a
load balancer or a reverse proxy, which is where certificate renewal already
lives. The `tls` block is the client half.

**The same block appears on gRPC, TCP, MQTT and the message-queue connectors**,
with the same four attributes and the same meaning.
