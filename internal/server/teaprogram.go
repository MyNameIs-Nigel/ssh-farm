package server

import (
	"bytes"
	"io"
	"runtime"

	tea "charm.land/bubbletea/v2"
	"charm.land/wish/v2/bubbletea"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/ssh"
)

// newTeaProgram builds the per-session Bubble Tea program. It exists (instead
// of letting the middleware call teaHandler directly) so Windows hosts can
// append option overrides after bubbletea.MakeOptions — options apply in
// order, and MakeOptions would otherwise win.
func (srv *Server) newTeaProgram(s ssh.Session) *tea.Program {
	model, _ := srv.teaHandler(s)
	if model == nil {
		return nil
	}
	opts := append(bubbletea.MakeOptions(s), windowsPtyOptions(s)...)
	return tea.NewProgram(withMouseCellMotion(model), opts...)
}

// withMouseCellMotion wraps a model so every view requests cell-motion mouse
// events. Bubble Tea v2 moved mouse mode from program options to View fields;
// this wrapper keeps the enablement in the server layer for tui/01.
func withMouseCellMotion(m tea.Model) tea.Model {
	return mouseModel{inner: m}
}

type mouseModel struct {
	inner tea.Model
}

func (m mouseModel) Init() tea.Cmd { return m.inner.Init() }

func (m mouseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.inner.Update(msg)
	if mm, ok := next.(mouseModel); ok {
		m.inner = mm.inner
	} else {
		m.inner = next
	}
	return m, cmd
}

func (m mouseModel) View() tea.View {
	v := m.inner.View()
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// windowsPtyOptions compensates for wish lacking Windows host support for
// emulated SSH PTYs (bubbletea/tea_other.go: "TODO: Support Windows PTYs").
// Two things differ from the unix path and both need patching:
//
//  1. Color profile: wish's unix build forces one for emulated PTYs
//     (tea_unix.go); the Windows build does not, so every lipgloss style is
//     stripped. We force it here.
//
//  2. Newline cursor movement: in alt-screen Bubble Tea's diff renderer moves
//     the cursor down one row by emitting a bare \n, and because mapNl is
//     forced off when GOOS == "windows" (tea.go), ultraviolet models the
//     cursor as staying in the SAME column afterwards (terminal_renderer.go:
//     tMapNewline leaves fx unchanged, so it skips the absolute horizontal
//     reposition when the next run starts in that same column). But Windows
//     consoles (Windows Terminal and classic conhost) treat \n as CR+LF, so
//     the cursor really jumps to column 0 — and incremental updates smear to
//     the left. We rewrite each movement \n to an explicit cursor-down
//     (ESC[B), which moves down keeping the column on every terminal, making
//     reality match the renderer's model. (A full redraw, e.g. on resize,
//     paints whole lines from column 0 and is unaffected — which is why
//     resizing visibly "fixes" the screen until the next partial update.)
//
// On unix hosts this returns nil.
func windowsPtyOptions(s ssh.Session) []tea.ProgramOption {
	if runtime.GOOS != "windows" {
		return nil
	}
	pty, _, ok := s.Pty()
	if !ok {
		return nil
	}
	envs := append(s.Environ(), "TERM="+pty.Term)
	return []tea.ProgramOption{
		tea.WithColorProfile(colorprofile.Env(envs)),
		tea.WithOutput(&cursorDownWriter{w: s}),
	}
}

// cursorDownWriter rewrites each newline to an explicit cursor-down sequence
// (ESC[B), which moves the cursor down one row without changing its column on
// any VT terminal. In Bubble Tea's alt-screen output every \n is a cursor
// movement (cell content is positioned absolutely, never written with literal
// newlines), so this is a safe, total substitution. A preceding \r is left in
// place: "\r\n" becomes "\r" + ESC[B (column 0, then down — unchanged meaning).
var cursorDown = []byte{0x1b, '[', 'B'}

type cursorDownWriter struct {
	w io.Writer
}

func (c *cursorDownWriter) Write(p []byte) (int, error) {
	if !bytes.ContainsRune(p, '\n') {
		return c.w.Write(p)
	}
	buf := make([]byte, 0, len(p)+bytes.Count(p, []byte{'\n'})*len(cursorDown))
	for _, b := range p {
		if b == '\n' {
			buf = append(buf, cursorDown...)
			continue
		}
		buf = append(buf, b)
	}
	if _, err := c.w.Write(buf); err != nil {
		return 0, err
	}
	return len(p), nil
}
