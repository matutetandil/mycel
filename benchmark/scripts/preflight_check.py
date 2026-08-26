"""Decide whether one benchmark response carries the data it should.

Reads the response body on stdin and the check expression from argv. Prints
nothing when the body is correct, and a one-line reason when it is not.
"""

import json
import sys


def main() -> int:
    raw = sys.stdin.read()
    check = sys.argv[1]

    try:
        parsed = json.loads(raw)
    except ValueError as exc:
        print("not JSON (%s): %s" % (exc, raw[:120]))
        return 0

    # CEL values that were never unwrapped serialize as {"Adapter":{}}. That is
    # valid JSON behind a valid 200, and it carries none of the data.
    if '"Adapter"' in raw:
        print("response contains unconverted CEL wrappers: %s" % raw[:160])
        return 0

    try:
        ok = bool(eval(check, {"__builtins__": {"any": any, "all": all, "len": len,
                                                "isinstance": isinstance, "dict": dict,
                                                "list": list, "str": str}},
                       {"r": parsed}))
    except Exception as exc:  # the check itself is part of what we are testing
        print("check raised %s over %s" % (exc, raw[:160]))
        return 0

    print("" if ok else "unexpected body: %s" % raw[:200])
    return 0


if __name__ == "__main__":
    sys.exit(main())
