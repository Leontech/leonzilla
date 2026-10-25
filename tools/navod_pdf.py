"""Generates the PDF guide from the built-in one (ui/help.js / ui/help_en.js + ui/app.css),
in the language the running Leonzilla is set to (Czech → Leonzilla_navod.pdf, English → Leonzilla_manual.pdf).

Usage: python navod_pdf.py <url of a running Leonzilla incl. #t=token> <output.pdf>
(Leonzilla in a browser: LEONZILLA_HEADLESS=1 LEONZILLA_PORT=18780 Leonzilla.exe prints the URL.)
Needs: pip install playwright && playwright install chromium
"""
import sys
from playwright.sync_api import sync_playwright

url, out = sys.argv[1], sys.argv[2]

PRINT_CSS = """
@page { size: A4; margin: 15mm 15mm 18mm; }
html, body { height: auto !important; overflow: visible !important; background: #fff !important; }
body { font-family: Inter, "Segoe UI", sans-serif; }
#app { display: block !important; height: auto !important; }
.bar, #activity, #overlay, #ctx, .toast, #toasts { display: none !important; }
.pdf .hp { display: block; height: auto; }
.pdf .hp-nav, .pdf .hp-close { display: none !important; }
.pdf .hp-body { overflow: visible; padding: 0; }
.pdf .hp-hero { padding-top: 6mm; }
.pdf .hp-hero h1 { font-size: 36px; }
.pdf section { break-inside: auto; margin-top: 16px; padding-top: 20px; }
.pdf h2 { break-after: avoid; }
.pdf .hp-btns > div, .pdf .hp-card, .pdf .hp-tab tr, .pdf .hp-steps li, .pdf .hp-list li, .pdf .hp-keys > div, .pdf .hp-tip { break-inside: avoid; }
.pdf .hp-thanks { margin: 28px 0 0; border-radius: 18px; border: 1px solid var(--line); break-inside: avoid; }
.pdf .hp-btns, .pdf .hp-cards, .pdf .hp-keys { grid-template-columns: 1fr 1fr !important; }
.pdf .hp-hero { grid-template-columns: auto 1fr !important; }
.pdf .hp-tab th:last-child, .pdf .hp-tab td:last-child { display: table-cell !important; }
.pdf .hp-tab td:first-child { white-space: nowrap; }
.pdf .hp-tab { font-size: 13px; }
.pdf-note { display: flex; gap: 14px; align-items: center; margin: 22px 0 6px; padding: 14px 18px; border-radius: 14px;
  background: var(--spruce-soft); border: 1px solid color-mix(in srgb, var(--spruce) 35%, transparent); font-size: 14px; line-height: 1.5; }
.pdf-note .q { width: 34px; height: 34px; flex: none; border-radius: 10px; display: grid; place-items: center; background: var(--spruce); color: var(--on-spruce); }
.pdf-note .q svg { width: 20px; height: 20px; }
.pdf-toc { margin: 18px 0 0; display: grid; grid-template-columns: 1fr 1fr; gap: 6px 26px; padding: 16px 18px; border: 1px solid var(--line); border-radius: 14px; break-inside: avoid; }
.pdf-toc h3 { grid-column: 1 / -1; margin: 0 0 4px; font: 600 11px Inter, sans-serif; text-transform: uppercase; letter-spacing: .08em; color: var(--faint); }
.pdf-toc a { color: var(--ink); text-decoration: none; font-size: 13.5px; display: flex; gap: 10px; padding: 3px 0; border-bottom: 1px dashed var(--line); }
.pdf-toc a .n { font: 600 11px var(--mono); color: var(--spruce); width: 18px; padding-top: 2px; }
"""

BUILD_JS = """() => {
  document.documentElement.setAttribute('data-theme', 'light');
  const app = document.querySelector('#app');
  app.innerHTML = '<div class="pdf">' + helpHTML() + '</div>';
  const hero = app.querySelector('.hp-hero');
  const note = document.createElement('div');
  note.className = 'pdf-note';
  const en = I18N.lang === 'en';
  note.innerHTML = '<span class="q"><svg><use href="#i-help"/></svg></span><span>' + (en
    ? 'This guide is also built into the app – in Leonzilla click the <b>?</b> button in the top bar (next to the settings) or press <b>F1</b>.'
    : 'Tento návod je i přímo součástí programu – v Leonzille klikni na tlačítko <b>?</b> v horní liště (vedle nastavení) nebo stiskni <b>F1</b>.') + '</span>';
  hero.after(note);
  const toc = document.createElement('nav');
  toc.className = 'pdf-toc';
  toc.innerHTML = '<h3>' + (en ? 'Contents' : 'Obsah') + '</h3>' + helpSections().map(([id, n], i) => `<a href="#hp-${id}"><span class="n">${String(i + 1).padStart(2, '0')}</span>${n}</a>`).join('');
  note.after(toc);
  app.querySelectorAll('a[data-url]').forEach((a) => a.removeAttribute('data-url'));
}"""

with sync_playwright() as pw:
    br = pw.chromium.launch()
    pg = br.new_page(color_scheme="light")
    pg.goto(url)
    pg.wait_for_function("typeof helpHTML === 'function' && S.version")
    pg.evaluate(BUILD_JS)
    pg.add_style_tag(content=PRINT_CSS)
    pg.wait_for_timeout(300)
    ver = pg.evaluate("S.version")
    word = "manual" if pg.evaluate("I18N.lang") == "en" else "návod"
    foot = f"""<div style="width:100%;font:9px Inter,sans-serif;color:#8a9a94;padding:0 15mm;display:flex;justify-content:space-between">
      <span>Leonzilla {ver} · {word}</span><span><span class="pageNumber"></span> / <span class="totalPages"></span></span></div>"""
    pg.pdf(path=out, format="A4", print_background=True, display_header_footer=True,
           header_template="<div></div>", footer_template=foot,
           margin={"top": "15mm", "bottom": "18mm", "left": "15mm", "right": "15mm"})
    br.close()
print("ok", out)
