// Package latex converts Markdown (with optional <!-- FIGURE/TABLE --> HTML comments)
// to a complete XeLaTeX document and renders it to PDF via xelatex.
package latex

import (
	"fmt"
	"strings"
)

const preamble = `\documentclass[a4paper,11pt]{article}
\usepackage{fontspec}
\setmainfont[
  Path=/usr/share/fonts/TTF/,
  Extension=.ttf,
  BoldFont=DejaVuSans-Bold,
  ItalicFont=DejaVuSans-Oblique
]{DejaVuSans}
\setmonofont[Path=/usr/share/fonts/TTF/,Extension=.ttf]{DejaVuSansMono}
\usepackage[top=2cm,bottom=2cm,left=2.2cm,right=2.2cm]{geometry}
\usepackage{xcolor}

\definecolor{codebg}{RGB}{245,245,245}
\definecolor{figurebg}{RGB}{232,241,255}
\definecolor{tablehintbg}{RGB}{240,255,240}
\definecolor{solutionbg}{RGB}{242,255,242}
\definecolor{primaryblue}{RGB}{25,75,200}
\definecolor{rulegray}{RGB}{180,180,180}
\definecolor{tableheadbg}{RGB}{25,75,200}

\usepackage{amsmath}
\usepackage{amssymb}
\usepackage{booktabs}
\usepackage{longtable}
\usepackage{tabularx}
\usepackage{ltablex}
\usepackage{array}
\usepackage{colortbl}
\keepXColumns
\usepackage[most,breakable]{tcolorbox}
\usepackage{listings}
\usepackage[normalem]{ulem}
\usepackage{parskip}
\usepackage{enumitem}
\usepackage{microtype}
\usepackage{titlesec}
\usepackage{fancyhdr}
\usepackage[colorlinks=true,linkcolor=primaryblue,urlcolor=primaryblue]{hyperref}
\usepackage{pdflscape}

\renewcommand{\arraystretch}{1.3}
\setlist[itemize]{noitemsep,topsep=3pt,parsep=0pt}
\setlist[enumerate]{noitemsep,topsep=3pt,parsep=0pt}
\setlist[itemize,2]{label=\textendash,leftmargin=1.5em}
\setlist[itemize,3]{label=\textbullet,leftmargin=1.5em}

\titleformat{\section}{\Large\bfseries\color{primaryblue}}{}{0em}{}
\titleformat{\subsection}{\large\bfseries\color{primaryblue!85!black}}{}{0em}{}
\titleformat{\subsubsection}{\normalsize\bfseries\color{primaryblue!70!black}}{}{0em}{}
\titlespacing*{\section}{0pt}{14pt}{4pt}
\titlespacing*{\subsection}{0pt}{10pt}{2pt}
\titlespacing*{\subsubsection}{0pt}{8pt}{1pt}

\pagestyle{fancy}
\fancyhf{}
\fancyfoot[C]{\small\thepage}
\renewcommand{\headrulewidth}{0pt}
\renewcommand{\footrulewidth}{0.3pt}

\newtcolorbox{figurebox}[1]{
  colback=figurebg,colframe=primaryblue!50,
  title={Abbildung: #1},fonttitle=\small\bfseries,
  breakable,before skip=6pt,after skip=6pt
}
\newtcolorbox{tablehintbox}[1]{
  colback=tablehintbg,colframe=green!50!black!50,
  title={Tabelle: #1},fonttitle=\small\bfseries,
  breakable,before skip=6pt,after skip=6pt
}
\newtcolorbox{solutionbox}[1]{
  colback=solutionbg,colframe=green!60!black!60,
  title={#1},fonttitle=\small\bfseries,
  breakable,before skip=6pt,after skip=6pt
}

\lstset{
  basicstyle=\ttfamily\small,
  backgroundcolor=\color{codebg},
  breaklines=true,
  frame=single,framerule=0pt,
  xleftmargin=4pt,xrightmargin=4pt,
  aboveskip=6pt,belowskip=6pt,
  keepspaces=true
}
`

// MarkdownToLatex converts Markdown with optional <!-- FIGURE/TABLE --> comments
// to a complete XeLaTeX source document.
func MarkdownToLatex(title, md string) string {
	body := convertBody(md)
	return fmt.Sprintf("%s\\title{\\textbf{%s}}\n\\date{}\n\\begin{document}\n\\maketitle\n\\tableofcontents\n\\newpage\n%s\n\\end{document}\n",
		preamble, escapeLatexStr(title), body)
}

// convertBody processes Markdown line by line and returns the LaTeX body content.
func convertBody(md string) string {
	var out strings.Builder
	lines := strings.Split(md, "\n")

	inCode := false

	// listStack tracks open list environments (innermost last).
	var listStack []string

	var tableRows []string

	// <details> collection state
	inDetails := false
	detailsSummary := ""
	var detailsLines []string

	// ── helpers ──────────────────────────────────────────────────────────────

	closeAllLists := func() {
		for len(listStack) > 0 {
			out.WriteString("\\end{" + listStack[len(listStack)-1] + "}\n")
			listStack = listStack[:len(listStack)-1]
		}
	}

	// adjustList opens/closes list environments to reach targetDepth with kind.
	adjustList := func(kind string, targetDepth int) {
		// Close excess levels
		for len(listStack) > targetDepth+1 {
			out.WriteString("\\end{" + listStack[len(listStack)-1] + "}\n")
			listStack = listStack[:len(listStack)-1]
		}
		// Open missing levels (always with requested kind)
		for len(listStack) < targetDepth+1 {
			out.WriteString("\\begin{" + kind + "}\n")
			listStack = append(listStack, kind)
		}
	}

	flushTable := func() {
		if len(tableRows) == 0 {
			return
		}
		renderLatexTable(&out, tableRows)
		tableRows = nil
	}

	// Pre-pass: convert setext headings to ATX style.
	// Only when the preceding line is non-empty — a blank line + --- is a horizontal rule, not a heading.
	for i := 1; i < len(lines); i++ {
		under := strings.TrimSpace(lines[i])
		prev := strings.TrimSpace(lines[i-1])
		if prev == "" {
			continue
		}
		if len(under) >= 2 && strings.Trim(under, "=") == "" {
			lines[i-1] = "# " + prev
			lines[i] = ""
		} else if len(under) >= 2 && strings.Trim(under, "-") == "" {
			lines[i-1] = "## " + prev
			lines[i] = ""
		}
	}

	for _, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)

		// ── <details> collection ─────────────────────────────────────────────
		if inDetails {
			if trimmed == "</details>" {
				// Render collected body through convertBody (recursive)
				innerMD := strings.Join(detailsLines, "\n")
				innerLatex := convertBody(innerMD)
				out.WriteString("\\begin{solutionbox}{" + escapeLatexStr(detailsSummary) + "}\n")
				out.WriteString(innerLatex)
				out.WriteString("\\end{solutionbox}\n\n")
				inDetails = false
				detailsSummary = ""
				detailsLines = nil
			} else if strings.HasPrefix(trimmed, "<summary>") && strings.HasSuffix(trimmed, "</summary>") {
				detailsSummary = trimmed[9 : len(trimmed)-10]
			} else if trimmed != "<details>" {
				// Skip the opening <details> line itself; collect everything else
				detailsLines = append(detailsLines, rawLine)
			}
			continue
		}

		// ── code fence ───────────────────────────────────────────────────────
		if strings.HasPrefix(trimmed, "```") {
			if inCode {
				out.WriteString("\\end{lstlisting}\n\n")
				inCode = false
			} else {
				flushTable()
				closeAllLists()
				lang := strings.TrimPrefix(trimmed, "```")
				if lang != "" && isKnownListingsLang(lang) {
					out.WriteString(fmt.Sprintf("\\begin{lstlisting}[language=%s]\n", escapeLatexStr(lang)))
				} else {
					out.WriteString("\\begin{lstlisting}\n")
				}
				inCode = true
			}
			continue
		}
		if inCode {
			out.WriteString(rawLine + "\n")
			continue
		}

		// ── start of <details> block ─────────────────────────────────────────
		if trimmed == "<details>" {
			flushTable()
			closeAllLists()
			inDetails = true
			detailsSummary = "Lösung"
			detailsLines = nil
			continue
		}

		// Standalone <summary> outside <details> — skip gracefully
		if strings.HasPrefix(trimmed, "<summary>") || trimmed == "</details>" {
			continue
		}

		// ── HTML comments ────────────────────────────────────────────────────
		if strings.HasPrefix(trimmed, "<!--") && strings.HasSuffix(trimmed, "-->") {
			flushTable()
			closeAllLists()
			inner := strings.TrimSpace(trimmed[4 : len(trimmed)-3])
			if after, ok := strings.CutPrefix(inner, "FIGURE:"); ok {
				desc := strings.TrimSpace(after)
				out.WriteString("\\begin{figurebox}{" + escapeLatexStr(desc) + "}\n")
				out.WriteString("\\textit{[Abbildung/Diagramm: " + escapeLatexStr(desc) + "]}\n")
				out.WriteString("\\end{figurebox}\n\n")
			} else if after, ok := strings.CutPrefix(inner, "TABLE:"); ok {
				desc := strings.TrimSpace(after)
				out.WriteString("\\begin{tablehintbox}{" + escapeLatexStr(desc) + "}\n")
				out.WriteString("\\textit{[Tabelle: " + escapeLatexStr(desc) + "]}\n")
				out.WriteString("\\end{tablehintbox}\n\n")
			}
			continue
		}

		// ── display math $$ ... $$ (single line) ─────────────────────────────
		if strings.HasPrefix(trimmed, "$$") && strings.HasSuffix(trimmed, "$$") && len(trimmed) > 4 {
			flushTable()
			closeAllLists()
			inner := trimmed[2 : len(trimmed)-2]
			out.WriteString("\\[\n" + inner + "\n\\]\n\n")
			continue
		}

		// ── Markdown tables ──────────────────────────────────────────────────
		if isMarkdownTableRow(trimmed) {
			closeAllLists()
			tableRows = append(tableRows, trimmed)
			continue
		}
		if len(tableRows) > 0 {
			flushTable()
		}

		// ── headings ─────────────────────────────────────────────────────────
		if text, ok := strings.CutPrefix(trimmed, "#### "); ok {
			closeAllLists()
			out.WriteString("\\paragraph{" + processInline(text) + "}\\mbox{}\\\\[2pt]\n")
			continue
		}
		if text, ok := strings.CutPrefix(trimmed, "### "); ok {
			closeAllLists()
			out.WriteString("\n\\subsubsection{" + processInline(text) + "}\n")
			continue
		}
		if text, ok := strings.CutPrefix(trimmed, "## "); ok {
			closeAllLists()
			out.WriteString("\n\\subsection{" + processInline(text) + "}\n")
			continue
		}
		if text, ok := strings.CutPrefix(trimmed, "# "); ok {
			closeAllLists()
			out.WriteString("\n\\section{" + processInline(text) + "}\n")
			continue
		}

		// ── horizontal rule ──────────────────────────────────────────────────
		if isHRule(trimmed) {
			closeAllLists()
			out.WriteString("\n\\noindent\\textcolor{rulegray}{\\rule{\\linewidth}{0.4pt}}\n\n")
			continue
		}

		// ── blockquote ───────────────────────────────────────────────────────
		if text, ok := strings.CutPrefix(trimmed, "> "); ok {
			closeAllLists()
			out.WriteString("\\begin{quote}\n" + processInline(text) + "\n\\end{quote}\n")
			continue
		}

		// ── list items (bullet or numbered, any indent depth) ────────────────
		if text, kind, depth, ok := cutListItem(rawLine); ok {
			flushTable()
			// Don't open a nested list before any top-level item exists —
			// LaTeX needs an \item before \begin{itemize}, otherwise it errors.
			// This handles markdown where indented bullets follow a paragraph.
			if len(listStack) == 0 && depth > 0 {
				depth = 0
			}
			adjustList(kind, depth)
			out.WriteString("  \\item " + processInline(text) + "\n")
			continue
		}

		// ── blank line ───────────────────────────────────────────────────────
		if trimmed == "" {
			closeAllLists()
			out.WriteString("\n")
			continue
		}

		// ── regular paragraph ────────────────────────────────────────────────
		closeAllLists()
		out.WriteString(processInline(trimmed) + "\n\n")
	}

	// close dangling environments
	if inCode {
		out.WriteString("\\end{lstlisting}\n")
	}
	closeAllLists()
	flushTable()
	if inDetails && len(detailsLines) > 0 {
		// unclosed details block — render what we have
		innerLatex := convertBody(strings.Join(detailsLines, "\n"))
		out.WriteString("\\begin{solutionbox}{" + escapeLatexStr(detailsSummary) + "}\n")
		out.WriteString(innerLatex)
		out.WriteString("\\end{solutionbox}\n\n")
	}

	return out.String()
}

// cutListItem detects a bullet or numbered list item in rawLine and returns
// (text, kind, indentDepth, ok). indentDepth is 0 for top-level, 1 for one
// level of nesting (2 spaces), 2 for two levels (4 spaces), etc.
func cutListItem(rawLine string) (text, kind string, depth int, ok bool) {
	spaces := 0
	for _, ch := range rawLine {
		if ch == ' ' {
			spaces++
		} else {
			break
		}
	}
	depth = spaces / 2
	trimmed := strings.TrimSpace(rawLine)

	if strings.HasPrefix(trimmed, "- ") {
		return trimmed[2:], "itemize", depth, true
	}
	if strings.HasPrefix(trimmed, "* ") {
		return trimmed[2:], "itemize", depth, true
	}
	// numbered: "1. ", "2. ", etc.
	for i, ch := range trimmed {
		if ch >= '0' && ch <= '9' {
			continue
		}
		if ch == '.' && i > 0 && len(trimmed) > i+2 && trimmed[i+1] == ' ' {
			return trimmed[i+2:], "enumerate", depth, true
		}
		break
	}
	return "", "", 0, false
}

// ─── inline formatting ────────────────────────────────────────────────────────

// processInline converts Markdown inline markers to LaTeX while escaping special
// characters. Math spans ($...$) are passed through without escaping so that
// LaTeX can render them directly.
func processInline(s string) string {
	var out strings.Builder
	b := []byte(s)
	i := 0
	for i < len(b) {
		// ~~strikethrough~~
		if i+1 < len(b) && b[i] == '~' && b[i+1] == '~' {
			if end := indexBytes(b[i+2:], []byte("~~")); end >= 0 {
				out.WriteString(`\sout{`)
				writeEscaped(&out, b[i+2:i+2+end])
				out.WriteString(`}`)
				i = i + 4 + end
				continue
			}
		}
		// [text](url) links
		if b[i] == '[' {
			if end := indexByte(b[i+1:], ']'); end >= 0 {
				rest := i + 2 + end
				if rest < len(b) && b[rest] == '(' {
					if urlEnd := indexByte(b[rest+1:], ')'); urlEnd >= 0 {
						linkText := b[i+1 : i+1+end]
						url := b[rest+1 : rest+1+urlEnd]
						out.WriteString(`\href{`)
						for _, c := range url {
							if c == '%' {
								out.WriteString(`\%`)
							} else {
								out.WriteByte(c)
							}
						}
						out.WriteString(`}{`)
						writeEscaped(&out, linkText)
						out.WriteString(`}`)
						i = rest + 2 + urlEnd
						continue
					}
				}
			}
		}
		// **bold**
		if i+1 < len(b) && b[i] == '*' && b[i+1] == '*' {
			if end := indexBytes(b[i+2:], []byte("**")); end >= 0 {
				out.WriteString(`\textbf{`)
				out.WriteString(processInline(string(b[i+2 : i+2+end])))
				out.WriteString(`}`)
				i = i + 4 + end
				continue
			}
		}
		// __bold__
		if i+1 < len(b) && b[i] == '_' && b[i+1] == '_' {
			if end := indexBytes(b[i+2:], []byte("__")); end >= 0 {
				out.WriteString(`\textbf{`)
				out.WriteString(processInline(string(b[i+2 : i+2+end])))
				out.WriteString(`}`)
				i = i + 4 + end
				continue
			}
		}
		// *italic* (not **)
		if b[i] == '*' && (i+1 >= len(b) || b[i+1] != '*') {
			if end := indexByte(b[i+1:], '*'); end >= 0 {
				out.WriteString(`\textit{`)
				out.WriteString(processInline(string(b[i+1 : i+1+end])))
				out.WriteString(`}`)
				i = i + 2 + end
				continue
			}
		}
		// `code`
		if b[i] == '`' {
			if end := indexByte(b[i+1:], '`'); end >= 0 {
				out.WriteString(`\texttt{`)
				writeEscaped(&out, b[i+1:i+1+end])
				out.WriteString(`}`)
				i = i + 2 + end
				continue
			}
		}
		// $math$ — pass content through unescaped so LaTeX renders it
		if b[i] == '$' {
			if end := indexByte(b[i+1:], '$'); end > 0 {
				inner := b[i+1 : i+1+end]
				if looksLikeMath(inner) {
					out.WriteByte('$')
					out.Write(inner)
					out.WriteByte('$')
					i = i + 2 + end
					continue
				}
			}
		}
		writeEscapedByte(&out, b[i])
		i++
	}
	return out.String()
}

// looksLikeMath returns true when the byte slice looks like a LaTeX math
// expression rather than a currency amount or plain text.
func looksLikeMath(b []byte) bool {
	// All-digit content (with optional comma/period/space) is likely currency
	allNumeric := true
	for _, c := range b {
		if c != '.' && c != ',' && c != ' ' && !(c >= '0' && c <= '9') {
			allNumeric = false
			break
		}
	}
	if allNumeric {
		return false
	}
	// Presence of typical math chars strongly suggests math mode
	for _, c := range b {
		switch c {
		case '_', '^', '\\', '{', '}', '+', '=', '<', '>', '/', '*', '(', ')':
			return true
		}
	}
	// Pure-alpha or alpha+digit content wrapped in $...$ — trust the user
	for _, c := range b {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return false
}

func isKnownListingsLang(lang string) bool {
	known := map[string]bool{
		"bash": true, "sh": true, "shell": true,
		"c": true, "c++": true, "cpp": true,
		"python": true, "java": true, "go": true,
		"sql": true, "html": true, "xml": true,
		"javascript": true, "js": true,
		"ruby": true, "perl": true, "php": true,
		"r": true, "matlab": true, "scala": true,
		"haskell": true, "lisp": true, "prolog": true,
	}
	return known[strings.ToLower(lang)]
}

func escapeLatexStr(s string) string {
	var out strings.Builder
	writeEscaped(&out, []byte(s))
	return out.String()
}

func writeEscaped(out *strings.Builder, b []byte) {
	for i := 0; i < len(b); {
		switch b[i] {
		case '\\':
			out.WriteString(`\textbackslash{}`)
			i++
		case '&':
			out.WriteString(`\&`)
			i++
		case '%':
			out.WriteString(`\%`)
			i++
		case '$':
			out.WriteString(`\$`)
			i++
		case '#':
			out.WriteString(`\#`)
			i++
		case '_':
			out.WriteString(`\_`)
			i++
		case '{':
			out.WriteString(`\{`)
			i++
		case '}':
			out.WriteString(`\}`)
			i++
		case '~':
			out.WriteString(`\textasciitilde{}`)
			i++
		case '^':
			out.WriteString(`\textasciicircum{}`)
			i++
		default:
			j := i + 1
			for j < len(b) {
				c := b[j]
				if c == '\\' || c == '&' || c == '%' || c == '$' || c == '#' ||
					c == '_' || c == '{' || c == '}' || c == '~' || c == '^' {
					break
				}
				j++
			}
			out.Write(b[i:j])
			i = j
		}
	}
}

func writeEscapedByte(out *strings.Builder, c byte) {
	writeEscaped(out, []byte{c})
}

func indexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}

func indexBytes(haystack, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			return i
		}
	}
	return -1
}

// ─── table rendering ─────────────────────────────────────────────────────────

func isMarkdownTableRow(s string) bool {
	return strings.HasPrefix(s, "|") && strings.HasSuffix(s, "|") && len(s) > 2
}

func isMarkdownSepRow(s string) bool {
	inner := strings.Trim(s, "|")
	for _, cell := range strings.Split(inner, "|") {
		cell = strings.TrimSpace(cell)
		if cell == "" {
			continue
		}
		for _, ch := range cell {
			if ch != '-' && ch != ':' {
				return false
			}
		}
	}
	return true
}

func parseTableCells(row string) []string {
	row = strings.Trim(row, "|")
	parts := strings.Split(row, "|")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}

// renderLatexTable renders a Markdown table as a tabularx table that fills the
// line width and wraps long cell content automatically.
// Tables with many columns are typeset in a smaller font and on a wider
// landscape page so headers and cells are not truncated.
func renderLatexTable(out *strings.Builder, rows []string) {
	var header []string
	var dataRows [][]string
	first := true
	for _, row := range rows {
		if isMarkdownSepRow(row) {
			continue
		}
		cells := parseTableCells(row)
		if first {
			header = cells
			first = false
		} else {
			dataRows = append(dataRows, cells)
		}
	}
	if len(header) == 0 {
		return
	}

	n := len(header)
	// X columns distribute the full linewidth equally and wrap text.
	colSpec := strings.Repeat("X", n)

	// Pick a font size based on column count: dense tables shrink to stay legible.
	fontCmd := "\\small"
	wide := n >= 8
	if n >= 10 {
		fontCmd = "\\scriptsize"
	} else if n >= 7 {
		fontCmd = "\\footnotesize"
	}

	if wide {
		// Rotate wide tables so each column has enough horizontal space.
		out.WriteString("\n\\begin{landscape}\n")
	}
	out.WriteString(fmt.Sprintf("\n{%s\n", fontCmd))
	out.WriteString(fmt.Sprintf("\\begin{tabularx}{\\linewidth}{%s}\n", colSpec))

	// Header row (repeated on each continuation page via \endhead)
	out.WriteString("\\hline\n")
	out.WriteString("\\rowcolor{tableheadbg}\n")
	for i, h := range header {
		if i > 0 {
			out.WriteString(" & ")
		}
		out.WriteString("{\\color{white}\\textbf{" + processInline(h) + "}}")
	}
	out.WriteString(" \\\\[2pt]\n\\hline\n")
	out.WriteString("\\endhead\n")          // repeat header on new pages
	out.WriteString("\\hline\n\\endfoot\n") // closing rule on each page

	for _, row := range dataRows {
		for i := 0; i < n; i++ {
			if i > 0 {
				out.WriteString(" & ")
			}
			if i < len(row) {
				out.WriteString(processInline(row[i]))
			}
		}
		out.WriteString(" \\\\\n")
	}

	out.WriteString("\\end{tabularx}\n}\n")
	if wide {
		out.WriteString("\\end{landscape}\n")
	}
	out.WriteString("\n")
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func isHRule(s string) bool {
	clean := strings.ReplaceAll(s, " ", "")
	return len(clean) >= 3 && (strings.Trim(clean, "-") == "" || strings.Trim(clean, "*") == "" || strings.Trim(clean, "_") == "")
}
