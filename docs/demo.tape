# The 60-second loop, recorded reproducibly.
#
#   docker build -f docs/demo.Dockerfile -t pix-sandbox-vhs .
#   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o .demo/pix-sandbox ./cmd/pix-sandbox
#   docker run --rm -v "$PWD:/vhs" -w /vhs pix-sandbox-vhs docs/demo.tape
#
# The tape starts the sandbox and a webhook receiver itself, so the recording
# needs nothing set up around it and cannot drift from what the software does.
# Every command below is real; nothing is typed for the camera that the
# software does not actually answer.

Output docs/demo.gif

Set Shell bash
Set FontSize 16
Set Width 1200
Set Height 640
Set Padding 20
Set TypingSpeed 30ms

# Setup, off camera: the emulator and an endpoint for its callbacks.
Hide
Type `export WEBHOOK_SECRET=demo-secret` Enter
Type `/vhs/.demo/pix-sandbox -db /tmp/demo.db > /tmp/sandbox.log 2>&1 &` Enter
Type `python3 /vhs/docs/demo-webhook.py > /tmp/hook.log 2>&1 &` Enter
Type `until curl -fsS localhost:8080/health >/dev/null 2>&1; do sleep 0.2; done` Enter
Type `clear` Enter
Show

Type `curl -s localhost:8080/health | jq`
Sleep 500ms
Enter
Sleep 2s

Type `# tell the sandbox where callbacks go`
Enter
Sleep 700ms
Type `curl -s -X PUT localhost:8080/webhook/dev@example.com -H 'Content-Type: application/json' -d '{"webhookUrl":"http://127.0.0.1:9099"}' | jq`
Sleep 500ms
Enter
Sleep 2500ms

Type `# create an immediate charge`
Enter
Sleep 700ms
Type `TXID=$(curl -s -X POST localhost:8080/cob -H 'Content-Type: application/json' -d '{"valor":{"original":"10.00"},"chave":"dev@example.com"}' | tee /tmp/cob.json | jq -r .txid)`
Sleep 500ms
Enter
Sleep 1s
Type `jq '{txid, status, pixCopiaECola}' /tmp/cob.json`
Sleep 300ms
Enter
Sleep 3500ms

Type `clear`
Enter
Sleep 300ms

Type `# a payer settles it - the half no BACEN endpoint covers`
Enter
Sleep 700ms
Type `E2E=$(curl -s -X POST localhost:8080/sandbox/pay -H 'Content-Type: application/json' -d "{\"txid\":\"$TXID\",\"infoPagador\":\"Coffee\"}" | jq -r .endToEndId)`
Sleep 500ms
Enter
Sleep 1500ms

Type `# the signed callback already landed`
Enter
Sleep 700ms
Type `jq . /tmp/hook.log`
Sleep 300ms
Enter
Sleep 4s

Type `clear`
Enter
Sleep 300ms

Type `# refund it in full`
Enter
Sleep 700ms
Type `curl -s -X PUT localhost:8080/pix/$E2E/devolucao/dev1 -H 'Content-Type: application/json' -d '{"valor":"10.00"}' | jq '{id, rtrId, status}'`
Sleep 500ms
Enter
Sleep 3s

Type `# every transition was recorded`
Enter
Sleep 700ms
Type `curl -s localhost:8080/cob/$TXID | jq -r .status`
Sleep 300ms
Enter
Sleep 2s

Type `# read the whole story at http://localhost:8080/console`
Enter
Sleep 3s
