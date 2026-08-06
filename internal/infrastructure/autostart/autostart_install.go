package autostart

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// shortcutName é o nome do atalho criado na área de trabalho pública.
const shortcutName = "FocusGuard.lnk"

// binaryNames são os executáveis copiados para o diretório de instalação
// quando presentes ao lado do instalador.
var binaryNames = []string{
	"focusguard.exe",
	"focusguard-daemon.exe",
	"focusguard-tray.exe",
	"focusguard-watchdog.exe",
	"focusguard-web.exe",
}

// InstallDir retorna o diretório de instalação do sistema
// (C:\Program Files\FocusGuard — Todos os Usuários). Em outras plataformas
// retorna vazio.
func InstallDir() string {
	if goos != "windows" {
		return ""
	}
	dir := os.Getenv("ProgramFiles")
	if dir == "" {
		dir = `C:\Program Files`
	}
	return filepath.Join(dir, "FocusGuard")
}

// InstallBinaries copia para o diretório de instalação todos os executáveis
// presentes em srcDir e retorna o diretório de instalação. A cópia de um
// binário em si mesma (src == dst, ex.: reinstalação a partir de Program
// Files) é ignorada.
func InstallBinaries(srcDir string) (string, error) {
	if goos != "windows" {
		return "", fmt.Errorf("autostart: instalação em Program Files é exclusiva do Windows")
	}
	installDir := InstallDir()
	if err := osMkdirAll(installDir, 0755); err != nil {
		return "", fmt.Errorf("autostart: falha ao criar diretório de instalação: %w", err)
	}
	for _, name := range binaryNames {
		src := filepath.Join(srcDir, name)
		dst := filepath.Join(installDir, name)
		if filepath.Clean(src) == filepath.Clean(dst) {
			continue
		}
		if _, err := osStat(src); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		if err := copyFile(src, dst); err != nil {
			return "", fmt.Errorf("autostart: falha ao copiar %s para %s: %w", src, dst, err)
		}
	}
	return installDir, nil
}

// EnsureInInstallDir copia o binário para o diretório de instalação quando o
// sistema já está instalado lá e retorna o novo caminho; caso contrário,
// retorna o caminho original (usado por install-tray / install-watchdog).
func EnsureInInstallDir(src string) (string, error) {
	if goos != "windows" {
		return src, nil
	}
	installDir := InstallDir()
	if _, err := osStat(installDir); err != nil {
		if os.IsNotExist(err) {
			return src, nil
		}
		return "", err
	}
	dst := filepath.Join(installDir, filepath.Base(src))
	if filepath.Clean(src) == filepath.Clean(dst) {
		return dst, nil
	}
	if err := copyFile(src, dst); err != nil {
		return "", fmt.Errorf("autostart: falha ao copiar %s para %s: %w", src, dst, err)
	}
	return dst, nil
}

// ShortcutPath retorna o caminho do atalho na área de trabalho pública
// (Todos os Usuários).
func ShortcutPath() string {
	pub := os.Getenv("PUBLIC")
	if pub == "" {
		pub = `C:\Users\Public`
	}
	return filepath.Join(pub, "Desktop", shortcutName)
}

// ExtractIcon extrai o ícone embutido do executável exePath para o arquivo
// icoPath usando ExtractAssociatedIcon (wrapper .NET da API Win32
// ExtractIconEx). O .ico resultante é usado como IconLocation do atalho da
// área de trabalho, em vez de apontar para o exe.
func ExtractIcon(exePath, icoPath string) error {
	if goos != "windows" {
		return fmt.Errorf("autostart: extração de ícone é exclusiva do Windows")
	}
	if !fileExists(exePath) {
		return fmt.Errorf("autostart: executável não encontrado para extrair ícone: %s", exePath)
	}

	// ExtractAssociatedIcon pode lançar exceção em vez de retornar $null
	// (ex.: certos ícones PNG-compressed); qualquer falha derruba o script e o
	// chamador cai no fallback (atalho aponta para o exe).
	script := fmt.Sprintf(
		`Add-Type -AssemblyName System.Drawing
$icon = $null
try {
  $icon = [System.Drawing.Icon]::ExtractAssociatedIcon(%s)
  if ($null -eq $icon) { throw 'ExtractAssociatedIcon retornou nulo' }
  $fs = [System.IO.File]::Create(%s)
  try { $icon.Save($fs) } finally { $fs.Dispose() }
} finally {
  if ($null -ne $icon) { $icon.Dispose() }
}`,
		psQuote(exePath), psQuote(icoPath),
	)

	out, err := execCommand("powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("autostart: falha ao extrair ícone de %s: %w (%s)", exePath, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CreateDesktopShortcut cria o atalho "FocusGuard" na área de trabalho
// pública apontando para target (o CLI instalado em Program Files). O ícone do
// atalho é o focusguard.ico extraído do executável (via ExtractIcon); se a
// extração falhar, o atalho aponta para o exe (o Explorer resolve o ícone
// embutido) como fallback.
func CreateDesktopShortcut(target string) error {
	if goos != "windows" {
		return fmt.Errorf("autostart: atalho de área de trabalho é exclusivo do Windows")
	}
	shortcut := ShortcutPath()
	if err := osMkdirAll(filepath.Dir(shortcut), 0755); err != nil {
		return fmt.Errorf("autostart: falha ao preparar área de trabalho: %w", err)
	}

	workDir := filepath.Dir(target)
	icon := target
	if daemon := filepath.Join(workDir, "focusguard-daemon.exe"); fileExists(daemon) {
		icon = daemon
	}

	// Extrai o ícone embutido para um focusguard.ico próprio (em vez de
	// apontar IconLocation para o exe). Fallback silencioso para o exe.
	icoPath := filepath.Join(workDir, "focusguard.ico")
	if err := ExtractIcon(icon, icoPath); err == nil {
		icon = icoPath
	}

	// WScript.Shell via PowerShell cria o atalho .lnk de verdade. Os caminhos
	// são inseridos com aspas simples (psQuote) para preservar as barras.
	script := fmt.Sprintf(
		`$ws = New-Object -ComObject WScript.Shell
$sc = $ws.CreateShortcut(%s)
$sc.TargetPath = %s
$sc.WorkingDirectory = %s
$sc.Description = %s
$sc.IconLocation = %s
$sc.Save()`,
		psQuote(shortcut), psQuote(target), psQuote(workDir),
		psQuote("FocusGuard - Bloqueio de distrações"), psQuote(icon),
	)

	out, err := execCommand("powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("autostart: falha ao criar atalho na área de trabalho: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RemoveDesktopShortcut remove o atalho da área de trabalho pública.
// Idempotente quando o atalho não existe.
func RemoveDesktopShortcut() error {
	p := ShortcutPath()
	if !fileExists(p) {
		return nil
	}
	if err := os.Remove(p); err != nil {
		return fmt.Errorf("autostart: falha ao remover atalho %s: %w", p, err)
	}
	return nil
}

// RemoveInstall remove o atalho e o diretório de instalação
// (C:\Program Files\FocusGuard).
func RemoveInstall() error {
	if goos != "windows" {
		return nil
	}
	if err := RemoveDesktopShortcut(); err != nil {
		return err
	}
	if err := os.RemoveAll(InstallDir()); err != nil {
		return fmt.Errorf("autostart: falha ao remover diretório de instalação: %w", err)
	}
	return nil
}

// psQuote envolve o valor em aspas simples do PowerShell, dobrando aspas
// simples internas.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func fileExists(p string) bool {
	_, err := osStat(p)
	return err == nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
