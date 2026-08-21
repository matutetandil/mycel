# Settings audit — ayuda memoria (NO commitear)

Archivo sin trackear, como IMPROVEMENTS.md. Cuidado con `git add -A`.

Método (reproducible): `/tmp/unread.go` escanea structs `*Config` buscando campos
escritos y nunca leídos, con AST e identidad de nodos; después se filtra contra
lecturas en todo el repo. Los de `auth` se verificaron escribiendo un `auth {}`
real y pasándolo por `mycel validate`.

Estado: ⬜ pendiente · 🔄 en curso · ✅ hecho · ⏸ aparcado por decisión del user

**Cerrado 2026-08-18: el escaneo sobre `internal/auth` no devuelve NADA.** Queda sólo 1.15.

---

## 1. Settings que nadie lee

### Auth (los que más pesan: son seguridad, y sus vecinos del mismo bloque sí funcionan)

| # | setting | bloque | qué promete | estado |
|---|---|---|---|---|
| 1.1 | `allow_list` | `sessions` | listar las sesiones propias | ✅ `8cf6f3f` |
| 1.2 | `allow_revoke` | `sessions` | revocar una sesión | ✅ `8cf6f3f` |
| 1.3 | `track` | `sessions` | qué se registra de cada sesión | ✅ `8cf6f3f` |
| 1.4 | `extend_on_activity` | `sessions` | extender la sesión al usarla | ✅ `6482647` |
| 1.5 | `history` | `password` | impedir reutilizar contraseñas | ✅ `4bbd165` |
| 1.6 | `max_age`, `warn_before` | `password` | forzar rotación | ✅ (ver git log) |
| 1.7 | `breach_check` | `password` | haveibeenpwned | ✅ |
| 1.8 | `max_speed_kmh`, `on_detect` | `security.impossible_travel` | bloque entero sin evaluar | ✅ |
| 1.9 | `max_devices`, `trust_duration`, `on_new_device` | `security.device_binding` | bloque entero sin evaluar | ✅ |
| 1.10 | `grace_period` (+ required, require_for, require_multiple, min_factors) | `mfa` | ✅ |
| 1.11 | `password_reset`, `password_forgot` | `endpoints` | ✅ `d0f6013` — los endpoints NO EXISTÍAN (404 con `Enabled: true`) |

Contraste que lo prueba: en `sessions`, `max_active` / `idle_timeout` /
`absolute_timeout` / `on_max_reached` **sí** se leen. Los otros cuatro no.

### Connectors

| # | setting | connector | nota | estado |
|---|---|---|---|---|
| 1.12 | `project_id`, `service_account_json` | push/FCM | ✅ `10a5385` — FCM v1 implementado; la API legacy la apagó Google en jun-2024 |
| 1.13 | `auto_generate` | graphql `schema` | ✅ `8a0bc1c` — un server sin schema arrancaba y respondía vacío |
| 1.14 | `grant_type` | http `auth` | ✅ `8a0bc1c` — ahora elige el grant; lo no implementado se rechaza |
| 1.15 | `TypeName` | graphql entity | ⏸ APARCADO — `EntityConfig` no lo referencia NADIE. Es la pregunta "muerto o falta el caller" |

## 2. Features documentadas que no existen

| # | qué | dónde | estado |
|---|---|---|---|
| 2.1 | `alert_only` | ejemplo de auth.md | ✅ reemplazado por on_detect |
| 2.2 | `fields` en device_binding | ejemplo de auth.md | ✅ ahora fingerprint |
| 2.3 | `claims` como bloque (no parsea) | ejemplo de auth.md | ✅ ahora atributo |

Los bloques de auth medio implementados (1.5–1.9) son esta misma categoría vista
desde la doc: parsean, están documentados con ejemplos, y no hacen nada.

Barrido: de los 89 atributos usados en ejemplos de `auth.md`, sólo esos 3 no
existen en el código. El problema no son nombres inventados.

### Encontrado sobre la marcha (item 1)

| # | qué | estado |
|---|---|---|
| 1.16 | `on_max_reached = "deny"` (¡documentado!) y `"revoke_all"` caían al `default` = revoke_oldest → hacían lo contrario de lo que dicen. Ahora `deny` = `reject_new` y lo desconocido se rechaza al arrancar | ✅ `8cf6f3f` |
| 1.17 | el bloque `sessions` no estaba descrito en `pkg/schema` (sin completions). Descrito, 8 attrs | ✅ `8cf6f3f` |
| 1.18 | el harness de paridad renderizaba el enum de un atributo **lista** como string suelto (`track = "ip"`) — nunca había habido un list-con-enum, así que no se notaba | ✅ `8cf6f3f` |

## 3. Defaults que se contradicen

**Ninguno verificado.** El escaneo comparando la columna "default" de las tablas
contra los defaults del código dio sólo ruido del regex. El único candidato serio
(`retry.attempts` doc=1 / código=0) se revisó a mano y está bien.

| # | qué | estado |
|---|---|---|
| 3.1 | Comentario obsoleto en `parseRetryBlock` | ✅ `8a0bc1c` |

---

## Ya cerrados en esta tanda (para no re-buscarlos)

- ✅ `publisher.confirms` (RabbitMQ) — publicaba sin esperar al broker
- ✅ `producer.topic` (Kafka) — configurarlo rompía toda publicación
- ✅ `producer.linger_ms` (Kafka) — 1.003s medidos por publicación
- ✅ `consumer.auto_commit` (Kafka) — un mensaje fallido quedaba salteado
- ✅ `keep_alive_interval` (GraphQL subs) — la suscripción moría a los 60s
- ✅ `ca_cert` (MQTT), `known_hosts` y `password` (ssh), `validate{}` de API keys,
  `sender_id`/`sms_type` (SNS), `passive` (FTP)
