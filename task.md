# 🛡️ FocusGuard v0.15.1 — Edição Server: validação em máquina real + correções

## 📌 1. Ideia Geral

A **v0.15.0** publicou dois instaladores Windows — `focusguard-<v>-amd64.msi`
(desktop) e `focusguard-server-<v>-amd64.msi` (edição Server, headless) — e
introduziu autenticação na interface web (usuários + login). A edição Server
é a mudança estrutural mais recente: um "aparelho" que instala só
daemon + watchdog + web + CLI, grava o marcador `server.role` ao lado do
daemon e, em **instalação limpa**, já nasce com o DNS sinkhole habilitado no
primeiro boot.

Esta task **valida a edição Server numa máquina Windows real** (instalação
limpa → DNS no 1º boot, login admin, gestão de usuários) e **fecha as lacunas
conhecidas** que ficaram da v0.15.0: não há comando CLI de recuperação de
senha (`focusguard user ...` — Parte E do plano da UI ficou de fora) e o
primeiro login não está documentado em lugar nenhum do repo.

## 🎯 2. Objetivos

- [ ] Validar o **MSI Server** numa máquina/VM Windows limpa: instalação,
      serviços, marcador `server.role` e **DNS ligado no 1º boot**.
- [ ] Validar o **login admin** e a gestão de usuários na interface web.
- [ ] Implementar a **recuperação de senha via CLI** (`focusguard user ...`).
- [ ] **Documentar o primeiro acesso** (README) — usuário `admin`, senha
      padrão da instalação, troca no 1º login e caminho de recuperação.
- [ ] Registrar e corrigir os achados dos testes (seção 6).

---

## 🖥️ 3. Testes manuais — MSI Server (instalação limpa)

> Checklist executável detalhado em `follow-up-v0.15.1.md`. Resumo:

### 3.1 Instalação
- [ ] Instalar `focusguard-server-<v>-amd64.msi` (admin) → pasta
      `C:\Program Files\FocusGuard Server\`.
- [ ] Serviços `FocusGuard` e `FocusGuardWatchdog` criados, Start = auto.
- [ ] **Sem** atalho no desktop, **sem** chave `Run` do tray; atalho no Menu
      Iniciar "FocusGuard Server" presente.
- [ ] `server.role` (vazio) ao lado do `focusguard-daemon.exe`.

### 3.2 DNS no 1º boot
- [ ] `%PROGRAMDATA%\FocusGuard\state.json` criado com `dns_enabled: true`.
- [ ] Porta 53 escutando (`netstat -ano | findstr :53` → `focusguard-daemon.exe`).
- [ ] `focusguard dns status` → listening + upstream padrão `1.1.1.2:53`.
- [ ] Consulta local: `nslookup <dominio> 127.0.0.1` encaminha ao upstream.
- [ ] Dispositivo na rede (celular/TV) com o DNS do roteador apontado para o
      PC: bloqueio funciona; site normal abre.
- [ ] Reinício da máquina: DNS volta sozinho (flag persistido).
- [ ] `focusguard dns stop` + reinício: DNS continua desligado (persistência).

### 3.3 Troca de sabor (mesmo UpgradeCode)
- [ ] Instalar o MSI **desktop** sobre o Server → vira desktop (tray volta,
      `server.role` some); registrar o que acontece com o DNS persistido.
- [ ] Instalar o MSI **Server** sobre o desktop → vira Server. Verificar lock
      do `tray.exe` em execução durante a troca (ver seção 5.3).

---

## 🔑 4. Testes manuais — login admin na UI

- [ ] `http://127.0.0.1:48902` → splash → tela de login.
- [ ] Entrar com `admin` + senha padrão da instalação; trocar a senha no 1º
      login (Configurações → Minha conta).
- [ ] Senha errada → "usuário ou senha inválidos"; 5 falhas → 429 (30s).
- [ ] Logout volta ao login; sessão expira em 12h.
- [ ] Gestão de usuários (admin): criar `maria`, trocar senha, remover;
      usuário comum só vê "Minha conta" (self-only; 403 na API direta).
- [ ] Recuperação: parar serviço, apagar `%PROGRAMDATA%\FocusGuard\user.json`,
      subir serviço → `admin` re-seedado com a senha padrão; arquivo recriado
      contém só hashes bcrypt.

---

## 🛠️ 5. Correções planejadas (lacunas conhecidas da v0.15.0)

### 5.1 CLI de recuperação de senha — `focusguard user ...`
A Parte E do plano da UI (nunca implementada) vira obrigatória agora:
- [ ] `focusguard user list` — lista os usuários (nomes).
- [ ] `focusguard user set-password <usuário>` — nova senha (interativa, sem
      eco; ou via flag) — funciona mesmo com o daemon/web fora do ar? **não**:
      via IPC como as demais ações (exige daemon ativo; documentar).
- [ ] Reusar as ações IPC existentes `user-list`/`user-set-password`
      (nenhuma mudança no daemon/IPC — só o comando CLI).
- [ ] Validar no README: caminho oficial de recuperação passa a ser o CLI,
      com o `user.json` apagado como último recurso.

### 5.2 Documentar o primeiro acesso (README)
- [ ] Seção "Primeiro login na interface web": usuário `admin`, senha padrão
      definida na instalação, troca no 1º acesso, rate limit e recuperação.
- [ ] Seção da **edição Server**: o que ela instala (headless), instalação
      limpa liga o DNS no 1º boot, conversão de instalação existente exige
      `focusguard dns start` (ou tela Rede).

### 5.3 (Avaliar) Lock do tray na troca desktop↔server
- [ ] Durante o `RemoveExistingProducts` da troca de sabor, o `tray.exe`
      (processo comum, iniciado pelo hook) pode estar com o arquivo travado →
      remoção falha ou agenda reboot. Se confirmado: encerrar o tray antes da
      troca (padrão do `stopForBinarySwap` do daemon) ou orientar na doc.

---

## ✅ 6. Critérios de aceite

- [ ] Todos os testes das seções 3 e 4 passam numa máquina Windows limpa
      (achados registrados na seção 8).
- [ ] `focusguard user list` e `focusguard user set-password` funcionam e
      estão no help da CLI.
- [ ] README documenta o primeiro login e a edição Server.
- [ ] Nenhuma regressão no MSI desktop nem no auto-update.

## 🧩 7. Escopo fora (não fazer nesta task)

- Multi-idioma na UI, telemetria, painel de dispositivos conectados ao
  sinkhole, HTTPS próprio do focusguard-web — ficam para versões futuras.

## 📝 8. Referências e achados

- `follow-up-v0.15.1.md` — checklist executável dos testes (seções 3 e 4).
- `docs/dns-sinkhole-spec.md` — spec técnica original do DNS sinkhole
  (arquivada; a task.md antiga).
- **Achados dos testes**: preencher aqui qualquer divergência (porta 53
  ocupada pelo ICS, atalho sem ícone, mensagens de erro, comportamento da
  troca de sabor, etc.) — cada achado vira issue/commit.
