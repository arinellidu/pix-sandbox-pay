# The 60-second loop, recorded reproducibly.
#
#   go install github.com/charmbracelet/vhs@latest   # needs ttyd + ffmpeg
#   vhs docs/demo.tape                               # writes docs/demo.gif
#
# Every command here is real. Nothing is typed for the camera that the
# software does not actually answer — a recording that disagrees with the
# binary is worse than no recording.
#
# Before recording, in another shell:
#   rm -rf /tmp/pix-demo && go run ./cmd/pix-sandbox -db /tmp/pix-demo/sandbox.db
#   npx -y http-echo-server 9099    # or any local echo endpoint

Output docs/demo.gif

Set Shell bash
Set FontSize 15
Set Width 1280
Set Height 760
Set Padding 24
Set TypingSpeed 40ms

Hide
Type "clear" Enter
Show

Type "# 1. tell the sandbox where callbacks go"
Enter
Sleep 800ms
Type "curl -s -X PUT :8080/webhook/dev@example.com -H 'Content-Type: application/json' -d '{\"webhookUrl\":\"http://127.0.0.1:9099/pix\"}' | jq"
Enter
Sleep 2s

Type "# 2. create an immediate charge"
Enter
Sleep 600ms
Type "TXID=$(curl -s -X POST :8080/cob -H 'Content-Type: application/json' -d '{\"valor\":{\"original\":\"10.00\"},\"chave\":\"dev@example.com\"}' | tee /tmp/cob.json | jq -r .txid)"
Enter
Sleep 1500ms
Type "jq '{txid, status, pixCopiaECola}' /tmp/cob.json"
Enter
Sleep 3s

Type "# 3. a payer settles it — this is the half no BACEN endpoint covers"
Enter
Sleep 600ms
Type "E2E=$(curl -s -X POST :8080/sandbox/pay -H 'Content-Type: application/json' -d \"{\\\"txid\\\":\\\"$TXID\\\",\\\"infoPagador\\\":\\\"Coffee\\\"}\" | tee /tmp/pix.json | jq -r .endToEndId)"
Enter
Sleep 1500ms
Type "jq '{endToEndId, valor, horario}' /tmp/pix.json"
Enter
Sleep 3s

Type "# the payee's endpoint already has the signed callback"
Enter
Sleep 2s

Type "# 4. refund it in full"
Enter
Sleep 600ms
Type "curl -s -X PUT :8080/pix/$E2E/devolucao/dev1 -H 'Content-Type: application/json' -d '{\"valor\":\"10.00\"}' | jq '{id, rtrId, status}'"
Enter
Sleep 3s

Type "# 5. the charge is closed, and every transition was recorded"
Enter
Sleep 600ms
Type "curl -s :8080/cob/$TXID | jq -r .status"
Enter
Sleep 2s

Type "# read the whole story at http://localhost:8080/console"
Enter
Sleep 3s
