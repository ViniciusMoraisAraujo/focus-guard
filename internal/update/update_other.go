//go:build !windows

package update

import "fmt"

// scheduleReplaceOnReboot troca targetPath por newPath no próximo boot — só o
// Windows suporta (MoveFileEx + MOVEFILE_DELAY_UNTIL_REBOOT). Em outras
// plataformas um binário em execução pode ser substituído na hora, então este
// fallback nunca é invocado lá. Stubbable nos testes.
var scheduleReplaceOnReboot = func(targetPath, newPath string) error {
	return fmt.Errorf("replace-on-reboot is only supported on Windows")
}
