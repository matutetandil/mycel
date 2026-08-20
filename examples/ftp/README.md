# FTP / SFTP

Reaching a partner's file server over HTTP: list what is there, fetch one file,
and put one back.

The connector speaks both FTP and SFTP; `protocol` chooses. This example uses
SFTP, which is the one that runs over SSH.

## Files

| File | Purpose |
|------|---------|
| `config.mycel` | Connectors and flows |

## What it does

```
GET  /files        → flow "list_files"    → the /reports directory
GET  /files/:path  → flow "download_file" → that file's contents
POST /files/upload → flow "upload_result" → a file written back
```

An upload names its file with `_filename` and carries it in `_content` — the
two fields the file connectors write by.

## Running

The file server is not part of the example. Point it at a partner's, or at one
of your own:

```bash
export SFTP_HOST=localhost SFTP_PORT=2222
export SFTP_USER=demo SFTP_PASS=demo

mycel start --config ./examples/ftp
```

For somewhere to point it at while trying this out:

```bash
docker run -d --rm -p 2222:22 -v "$PWD/outgoing:/home/demo/incoming" \
  atmoz/sftp demo:demo:::incoming
```

## Try It

```bash
curl http://localhost:3000/files

curl http://localhost:3000/files/report.csv

curl -X POST http://localhost:3000/files/upload \
  -H "Content-Type: application/json" \
  -d '{"filename":"result.csv","content":"id,total\n1,99\n"}'
```
