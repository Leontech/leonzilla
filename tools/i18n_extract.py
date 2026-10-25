"""Lists the Czech UI texts of ui/app.js, ui/about.js and ui/index.html that have no English translation in ui/lang_en.js.

The UI is written in Czech; English is applied at run time by ui/i18n.js (it translates the text
nodes and the title / placeholder / aria-label attributes the UI renders). Keys are Czech texts
with "{}" in place of values filled in by the code.

Usage:  python tools/i18n_extract.py            -> missing keys (JSON) on stdout
        python tools/i18n_extract.py --all      -> all keys found in app.js
"""
import html
import json
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CZ = re.compile(r"[áčďéěíňóřšťúůýžÁČĎÉĚÍŇÓŘŠŤÚŮÝŽ]")
# Czech words without diacritics that appear in the UI
CZW = re.compile(r"(?i)\b(nebo|karta|karty|karet|kartu|bod|body|skupin[ay]?|skupinu|projekty?|varianty?|variant|selhal|selhalo|kontroly?|kontrol|zkontrolovat|synchroniz\w*|protokol|heslo|zastavit|zastavuji|seznam|konfigurace|odebrat|konflikt|neprojito|commity|hotovo|konec|chyba|chyb|akce|vymazat|odeslat|spustit|ano|ne|vypnuto|naposledy|nenalezeno|nic|znovu|vzhled|nastavit|upravit|upravuje|dnes|odemknout|duplikovat|smazat|bez|spravovat|skript|jen|parametry|posunout|nahoru|vybrat|vyber|zahrnout|zkusit|podle|nalezen|nepovedla|nepovedlo|info k verzi|zkompiluje|nahraje|monitor –|rychlost|odebrat|aktivace|kopie|tuto|dvojici|buildy|commitu|varianty)\b")
MARK = "\x00"


def scan_js(src):
    """Yields ('str', text) for string literals and ('tpl', skeleton, [nested sources]) for templates."""
    i, n = 0, len(src)
    prev = ""  # last significant char (regex detection)
    out = []
    while i < n:
        c = src[i]
        if c in " \t\r\n":
            i += 1
            continue
        if src.startswith("//", i):
            i = src.find("\n", i)
            i = n if i < 0 else i
            continue
        if src.startswith("/*", i):
            i = src.find("*/", i + 2)
            i = n if i < 0 else i + 2
            continue
        if c in "\"'":
            j, buf = i + 1, []
            while j < n and src[j] != c:
                if src[j] == "\\":
                    buf.append(src[j:j + 2])
                    j += 2
                    continue
                buf.append(src[j])
                j += 1
            raw = "".join(buf)
            try:
                txt = json.loads('"' + raw.replace('"', '\\"').replace("\\'", "'") + '"')
            except Exception:
                txt = raw
            out.append(("str", txt))
            i, prev = j + 1, "a"
            continue
        if c == "`":
            skel, nested, j = parse_tpl(src, i + 1)
            out.append(("tpl", skel))
            for e in nested:
                out.extend(scan_js(e))
            i, prev = j, "a"
            continue
        if c == "/" and (prev == "" or prev in "(,=:[!&|?{};+-*%<>~^" or re.search(r"(return|typeof|case)\s*$", src[max(0, i - 8):i])):
            j, cls = i + 1, False
            while j < n:
                if src[j] == "\\":
                    j += 2
                    continue
                if src[j] == "[":
                    cls = True
                elif src[j] == "]":
                    cls = False
                elif src[j] == "/" and not cls:
                    break
                elif src[j] == "\n":
                    break
                j += 1
            i, prev = j + 1, "a"
            continue
        prev = c if not (c.isalnum() or c in "_$") else "a"
        i += 1
    return out


def parse_tpl(src, i):
    """Parses a template literal starting after the backtick. Returns (skeleton, nested expr sources, end)."""
    skel, nested, n = [], [], len(src)
    while i < n:
        c = src[i]
        if c == "\\":
            nxt = src[i + 1]
            skel.append({"n": "\n", "t": "\t", "`": "`", "\\": "\\", "$": "$"}.get(nxt, nxt))
            i += 2
            continue
        if c == "`":
            return "".join(skel), nested, i + 1
        if src.startswith("${", i):
            depth, j = 1, i + 2
            while j < n and depth:
                ch = src[j]
                if ch == "`":
                    _, _, j = parse_tpl(src, j + 1)
                    continue
                if ch in "\"'":
                    k = j + 1
                    while k < n and src[k] != ch:
                        k += 2 if src[k] == "\\" else 1
                    j = k + 1
                    continue
                if ch == "{":
                    depth += 1
                elif ch == "}":
                    depth -= 1
                j += 1
            nested.append(src[i + 2:j - 1])
            skel.append(MARK)
            i = j
            continue
        skel.append(c)
        i += 1
    return "".join(skel), nested, i


TAG = re.compile(r"<[^<>]*>")
ATTR = re.compile(r'\b(title|placeholder|aria-label)="([^"]*)"')


def norm(s):
    s = html.unescape(s)
    s = re.sub(r"\s+", " ", s).strip()
    s = re.sub(MARK + r"(\s*" + MARK + r")+", MARK, s)
    return s.replace(MARK, "{}")


def keys_of(text):
    """Text pieces of a string / template skeleton that end up as separate text nodes or attributes."""
    keys = []
    for tag in TAG.findall(text):
        for _, v in ATTR.findall(tag):
            keys.append(norm(v))
    for seg in TAG.split(text):
        keys.append(norm(seg))
    return [k for k in keys if (CZ.search(k) or CZW.search(k)) and not re.match(r"^[#.\[]|^/api", k)]


def collect(path):
    src = open(path, encoding="utf8").read()
    found = []
    for item in scan_js(src):
        for k in keys_of(item[1]):
            if k not in found:
                found.append(k)
    return found


def known_keys():
    p = os.path.join(ROOT, "ui", "lang_en.js")
    if not os.path.isfile(p):
        return set()
    m = re.search(r"/\*BEGIN\*/([\s\S]*)/\*END\*/", open(p, encoding="utf8").read())
    return set(json.loads(m.group(1)).keys()) if m else set()


if __name__ == "__main__":
    allk = collect(os.path.join(ROOT, "ui", "app.js"))
    for k in collect(os.path.join(ROOT, "ui", "about.js")):
        if k not in allk:
            allk.append(k)
    for k in keys_of(open(os.path.join(ROOT, "ui", "index.html"), encoding="utf8").read()):
        if k not in allk:
            allk.append(k)
    if "--all" not in sys.argv:
        k = known_keys()
        allk = [x for x in allk if x not in k]
    json.dump({x: "" for x in allk}, sys.stdout, ensure_ascii=False, indent=1)
    print()
