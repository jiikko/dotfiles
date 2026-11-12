"""記録に入れる Python と unicodedata のバージョンを JSON で出す。"""
import json
import sys
import unicodedata

json.dump({"python_version": ".".join(str(v) for v in sys.version_info[:3]),
           "unicodedata_version": unicodedata.unidata_version}, sys.stdout, separators=(",", ":"))
