//go:build windows

package winfocus

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindow                = user32.NewProc("GetWindow")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW     = user32.NewProc("GetWindowTextLengthW")
	procShowWindow               = user32.NewProc("ShowWindow")
	procIsIconic                 = user32.NewProc("IsIconic")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop         = user32.NewProc("BringWindowToTop")
	procAttachThreadInput        = user32.NewProc("AttachThreadInput")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procIsWindow                 = user32.NewProc("IsWindow")
	procSwitchToThisWindow       = user32.NewProc("SwitchToThisWindow")

	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procGetCurrentThreadId = kernel32.NewProc("GetCurrentThreadId")
)

const (
	gwOwner   = 4 // GW_OWNER
	swRestore = 9 // SW_RESTORE
)

// winInfo is a snapshot of one visible, owner-less top-level window.
type winInfo struct {
	hwnd  uintptr
	pid   uint32
	title string
}

// parentMap returns a child-PID -> parent-PID table for all running processes.
func parentMap() map[uint32]uint32 {
	out := map[uint32]uint32{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err != nil {
		return out
	}
	for {
		out[pe.ProcessID] = pe.ParentProcessID
		if err := windows.Process32Next(snap, &pe); err != nil {
			break
		}
	}
	return out
}

// enumTopLevelWindows snapshots every visible, owner-less top-level window with
// its owning PID and title. EnumWindows only walks top-level windows; the
// owner-less filter drops tool windows and dialogs, keeping main app windows.
func enumTopLevelWindows() []winInfo {
	var list []winInfo
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
			return 1 // keep enumerating
		}
		if owner, _, _ := procGetWindow.Call(hwnd, gwOwner); owner != 0 {
			return 1
		}
		var pid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		list = append(list, winInfo{hwnd: hwnd, pid: pid, title: windowText(hwnd)})
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return list
}

func windowText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, int(n)+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf)
}

// windowForPID returns a window owned by pid, preferring one with a title.
func windowForPID(list []winInfo, pid uint32) uintptr {
	if pid == 0 {
		return 0
	}
	var fallback uintptr
	for _, w := range list {
		if w.pid != pid {
			continue
		}
		if w.title != "" {
			return w.hwnd
		}
		if fallback == 0 {
			fallback = w.hwnd
		}
	}
	return fallback
}

func windowByTitleContains(list []winInfo, needle string) uintptr {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return 0
	}
	lower := strings.ToLower(needle)
	for _, w := range list {
		if w.title != "" && strings.Contains(strings.ToLower(w.title), lower) {
			return w.hwnd
		}
	}
	return 0
}

func isWindow(hwnd uintptr) bool {
	r, _, _ := procIsWindow.Call(hwnd)
	return r != 0
}

func pidForWindow(hwnd uintptr) uint32 {
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

// CaptureFocusContext walks up the process tree from the current (hook) process
// to the nearest ancestor that owns a visible top-level window — the terminal
// window hosting Claude (e.g. WindowsTerminal.exe, Code.exe, conhost). It
// records the handle, owning PID, title and project folder for later focus.
func CaptureFocusContext(cwd string) (FocusContext, bool) {
	folder := ""
	if cwd != "" {
		folder = filepath.Base(cwd)
	}

	list := enumTopLevelWindows()
	parents := parentMap()

	// A single process can own several top-level windows — notably the Windows
	// Terminal "monarch" hosting multiple windows. When the foreground window
	// belongs to the terminal process we resolve to, prefer it: it is almost
	// always the window the user was looking at when Claude produced output.
	fg, _, _ := procGetForegroundWindow.Call()
	fgPID := uint32(0)
	if fg != 0 {
		fgPID = pidForWindow(fg)
	}

	pid := windows.GetCurrentProcessId()
	seen := map[uint32]bool{}
	for i := 0; i < 32 && pid != 0 && !seen[pid]; i++ {
		seen[pid] = true

		hwnd := uintptr(0)
		if fg != 0 && fgPID == pid {
			hwnd = fg
		}
		if hwnd == 0 {
			hwnd = windowForPID(list, pid)
		}
		if hwnd != 0 {
			return FocusContext{
				HWND:   int64(hwnd),
				PID:    pid,
				Title:  windowText(hwnd),
				Folder: folder,
			}, true
		}
		pid = parents[pid]
	}

	// No ancestor owns a window (fully detached hook). Still hand back a
	// folder-only context so the click handler can attempt a title match.
	if folder != "" {
		return FocusContext{Folder: folder}, true
	}
	return FocusContext{}, false
}

// resolveWindow turns a (possibly stale) FocusContext back into a live HWND.
// Order: validated stored handle, then owning PID, then title/folder match.
func resolveWindow(ctx FocusContext) uintptr {
	list := enumTopLevelWindows()

	if ctx.HWND != 0 {
		hwnd := uintptr(ctx.HWND)
		if isWindow(hwnd) && (ctx.PID == 0 || pidForWindow(hwnd) == ctx.PID) {
			return hwnd
		}
	}
	if hwnd := windowForPID(list, ctx.PID); hwnd != 0 {
		return hwnd
	}
	for _, needle := range []string{ctx.Folder, ctx.Title} {
		if hwnd := windowByTitleContains(list, needle); hwnd != 0 {
			return hwnd
		}
	}
	return 0
}

// Focus raises the terminal window described by ctx to the foreground.
func Focus(ctx FocusContext) error {
	hwnd := resolveWindow(ctx)
	if hwnd == 0 {
		return fmt.Errorf("winfocus: no matching window for %+v", ctx)
	}
	forceForeground(hwnd)
	return nil
}

// forceForeground raises hwnd, defeating Windows' foreground-stealing lock by
// briefly attaching our input queue to the current foreground thread (the
// well-known AttachThreadInput recipe).
func forceForeground(hwnd uintptr) {
	// Only un-minimize when actually minimized; SW_RESTORE keeps a prior
	// maximized state. Don't call ShowWindow on an already-visible window — we
	// only resolve visible windows, so SW_SHOW is redundant and any show/normalize
	// call can knock a maximized or full-screen terminal out of its layout.
	if r, _, _ := procIsIconic.Call(hwnd); r != 0 {
		procShowWindow.Call(hwnd, swRestore)
	}

	fg, _, _ := procGetForegroundWindow.Call()
	curThread, _, _ := procGetCurrentThreadId.Call()
	var fgThread uintptr
	if fg != 0 {
		fgThread, _, _ = procGetWindowThreadProcessId.Call(fg, 0) // 0 == NULL lpdwProcessId
	}

	attached := false
	if fgThread != 0 && fgThread != curThread {
		if r, _, _ := procAttachThreadInput.Call(curThread, fgThread, 1); r != 0 {
			attached = true
		}
	}
	procBringWindowToTop.Call(hwnd)
	// SwitchToThisWindow raises like Alt+Tab — it helps defeat the foreground
	// lock and, unlike a show/normalize call, doesn't disturb the window's
	// maximized/full-screen state. Undocumented but stable in user32 for decades.
	procSwitchToThisWindow.Call(hwnd, 1)
	procSetForegroundWindow.Call(hwnd)
	if attached {
		procAttachThreadInput.Call(curThread, fgThread, 0)
	}
}

// EnsureRegistered registers (or refreshes) the click-to-focus URI handler under
// HKCU pointing at the currently running executable. Idempotent: a no-op when
// the stored command already matches.
func EnsureRegistered() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if abs, err := filepath.Abs(exe); err == nil {
		exe = abs
	}
	want := commandValue(exe)

	cmdPath := `Software\Classes\` + ProtocolScheme + `\shell\open\command`
	if k, err := registry.OpenKey(registry.CURRENT_USER, cmdPath, registry.QUERY_VALUE); err == nil {
		cur, _, _ := k.GetStringValue("")
		k.Close()
		if cur == want {
			return nil
		}
	}
	return RegisterProtocolHandler(exe)
}

func commandValue(exe string) string {
	return fmt.Sprintf(`"%s" focus-windows "%%1"`, exe)
}

// RegisterProtocolHandler writes the HKCU\Software\Classes\<scheme> keys that
// let Windows launch exe with the click-to-focus URI. Per-user; no admin rights.
func RegisterProtocolHandler(exe string) error {
	base := `Software\Classes\` + ProtocolScheme
	root, _, err := registry.CreateKey(registry.CURRENT_USER, base, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.SetStringValue("", "URL:Claude Notifications Focus"); err != nil {
		return err
	}
	if err := root.SetStringValue("URL Protocol", ""); err != nil {
		return err
	}

	cmd, _, err := registry.CreateKey(registry.CURRENT_USER, base+`\shell\open\command`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer cmd.Close()
	return cmd.SetStringValue("", commandValue(exe))
}
