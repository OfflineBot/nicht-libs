package latex

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// RenderPDF compiles a XeLaTeX source document to PDF and returns the PDF bytes.
// xelatex is run twice to resolve any internal references.
func RenderPDF(latexSrc []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "zettel-latex-*")
	if err != nil {
		return nil, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	texFile := filepath.Join(dir, "zettel.tex")
	if err := os.WriteFile(texFile, latexSrc, 0o644); err != nil {
		return nil, fmt.Errorf("write tex: %w", err)
	}

	run := func() ([]byte, error) {
		cmd := exec.Command("xelatex",
			"-interaction=nonstopmode",
			"-halt-on-error",
			"-output-directory="+dir,
			texFile,
		)
		cmd.Dir = dir
		return cmd.CombinedOutput()
	}

	// First pass
	out, err := run()
	if err != nil {
		return nil, fmt.Errorf("xelatex pass 1 failed: %w\n%s", err, truncateLog(out))
	}

	// Second pass (resolves section numbers, etc.)
	out, err = run()
	if err != nil {
		return nil, fmt.Errorf("xelatex pass 2 failed: %w\n%s", err, truncateLog(out))
	}

	pdfPath := filepath.Join(dir, "zettel.pdf")
	pdf, err := os.ReadFile(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("read pdf: %w\nxelatex output:\n%s", err, truncateLog(out))
	}
	return pdf, nil
}

// truncateLog keeps at most the last 3000 bytes of log output for error messages.
func truncateLog(b []byte) []byte {
	const max = 3000
	if len(b) <= max {
		return b
	}
	return b[len(b)-max:]
}
