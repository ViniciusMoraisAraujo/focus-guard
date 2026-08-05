//go:build windows

package update

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// moveFileExDelayUntilReboot (MOVEFILE_DELAY_UNTIL_REBOOT) manda o MoveFileExW
// executar o rename apenas na próxima inicialização do sistema — a operação é
// registrada em PendingFileRenameOperations e o SO a executa antes de qualquer
// serviço subir, quando nenhum binário está em execução.
const moveFileExDelayUntilReboot = 0x4

// scheduleReplaceOnReboot agenda a troca de targetPath por newPath no próximo
// boot via MoveFileExW + MOVEFILE_DELAY_UNTIL_REBOOT. Exige token elevado (o
// daemon roda com manifest requireAdministrator) e paths absolutos. O arquivo
// newPath precisa estar no mesmo volume do alvo (o estágio ".<nome>.new" fica
// ao lado do binário, garantindo isso). Stubbable nos testes para exercitar o
// caminho do fallback em qualquer plataforma.
var scheduleReplaceOnReboot = func(targetPath, newPath string) error {
	target, err := windows.UTF16PtrFromString(targetPath)
	if err != nil {
		return err
	}
	source, err := windows.UTF16PtrFromString(newPath)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(source, target, moveFileExDelayUntilReboot); err != nil {
		return fmt.Errorf("MoveFileEx %s → %s (on reboot): %w", newPath, targetPath, err)
	}
	return nil
}
