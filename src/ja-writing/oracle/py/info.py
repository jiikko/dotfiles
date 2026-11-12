"""記録に入れる Python と unicodedata のバージョンを JSON で出す。
python_version は 3.15.0 のように、final 以外は prerelease の区別 (a・b・rc と番号) も付ける (3.15.0rc1 と 3.15.0 を同じ環境として扱わない)。"""
import json
import sys
import unicodedata

info = sys.version_info
suffix = "" if info.releaselevel == "final" else {"alpha": "a", "beta": "b", "candidate": "rc"}[info.releaselevel] + str(info.serial)
json.dump({"python_version": "%d.%d.%d%s" % (info.major, info.minor, info.micro, suffix),
           "unicodedata_version": unicodedata.unidata_version}, sys.stdout, separators=(",", ":"))
