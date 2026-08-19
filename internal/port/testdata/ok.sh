#!/bin/sh
# Echo the envelope back with the payload field surfaced as "got".
python3 -c '
import json, sys
req = json.load(sys.stdin)
print(json.dumps({
    "evol": req["evol"], "port": req["port"], "action": req["action"],
    "got": req.get("payload", ""),
}))
'
