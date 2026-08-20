#!/bin/sh
# Reflects EVOL_TEST_VALUE back so tests can observe the adapter's
# environment. Reads and discards stdin per the wire protocol.
cat >/dev/null
printf '{"evol":"1","port":"echo","action":"env","value":"%s"}\n' "${EVOL_TEST_VALUE:-unset}"
