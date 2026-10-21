# Generate golden vectors by running the ORIGINAL Python wr_hash implementation
# (copied verbatim from weread_exporter/utils.py) — used by Go tests.
import hashlib, json, random
from typing import List

def md5(s):
    if not isinstance(s, bytes):
        s = s.encode()
    return hashlib.md5(s).hexdigest()

def wr_hash(s: str) -> str:
    hash_str: str = md5(s)
    result: str = hash_str[:3] + "32" + hash_str[-2:]
    _0x22edbf: List[str] = []
    for i in range(0, len(s), 9):
        _0x22edbf.append("%x" % int(s[i : min(i + 9, len(s))]))

    for i, it in enumerate(_0x22edbf):
        _0x116344: str = "%x" % len(it)
        if len(_0x116344) == 1:
            _0x116344 = "0" + _0x116344
        result += _0x116344 + it
        if i < len(_0x22edbf) - 1:
            result += "g"

    if len(result) < 20:
        result += hash_str[: 20 - len(result)]
    result += hashlib.md5(result.encode()).hexdigest()[:3]
    return result

random.seed(20261022)
inputs = ["0", "1", "7", "14", "41", "49", "51", "99", "100", "123456789", "1234567890", "000123456",
          "42557145", "34336681", "000000000", "999999999", "1000000000", "123456789012345678",
          "00000000000000000", "12345678901234567890123"]
for n in range(1, 25):
    inputs.append("".join(random.choice("0123456789") for _ in range(n)))
for _ in range(60):
    inputs.append(str(random.randint(1, 10**random.randint(1, 12))))
seen = set(); uniq = []
for i in inputs:
    if i not in seen:
        seen.add(i); uniq.append(i)
out = [{"in": i, "out": wr_hash(i)} for i in uniq]
assert wr_hash("42557145") == "f343248072895ed9f34f408"
assert wr_hash("14") == "aab325601eaab3238922e53"
print(json.dumps(out, indent=1))
