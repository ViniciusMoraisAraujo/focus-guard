# 🔁 Follow-up v0.15.1 — Testes manuais da edição Server + login admin

> Checklist de validação da **v0.15.0** para a próxima versão. Foco: instalar o
> **MSI Server** numa máquina Windows (instalação limpa → DNS liga no 1º boot)
> e validar o **login admin** na interface web. Testes manuais — não são
> cobertos pelas suítes de teste automatizadas (exigem Windows + WiX + rede).

---

## 📋 0. Pré-requisitos

- [ ] Máquina/VM Windows **limpa** (sem FocusGuard instalado; sem
      `%PROGRAMDATA%\FocusGuard\state.json`).
- [ ] Baixar os **2 MSIs** da release v0.15.0:
      `focusguard-0.15.0-amd64.msi` e `focusguard-server-0.15.0-amd64.msi`.
- [ ] (Opcional) um celular/TV/tablet na mesma rede para validar o sinkhole.

---

## 🖥️ 1. MSI Server — instalação limpa → DNS no 1º boot

### 1.1 Instalar

- [ ] Rodar `focusguard-server-0.15.0-amd64.msi` (admin).
- [ ] Instalou em **`C:\Program Files\FocusGuard Server\`** (nome do produto
      vira o nome da pasta).
- [ ] Serviços criados e rodando: `sc query FocusGuard` e
      `sc query FocusGuardWatchdog` (Start = auto).
- [ ] **Sem** atalho no desktop e **sem** chave `Run` do tray
      (`reg query HKCU\Software\Microsoft\Windows\CurrentVersion\Run` não
      lista `FocusGuardTray`).
- [ ] Menu Iniciar tem o atalho **"FocusGuard Server"** (abre a interface web).

### 1.2 Marcador server.role

- [ ] `dir "C:\Program Files\FocusGuard Server\server.role"` — arquivo vazio
      existe ao lado do `focusguard-daemon.exe`.

### 1.3 DNS liga no 1º boot (instalação limpa)

- [ ] `%PROGRAMDATA%\FocusGuard\state.json` foi criado com `dns_enabled: true`
      (o daemon detectou o marcador no primeiro boot e persistiu o flag).
- [ ] Porta 53 escutando: `netstat -ano | findstr :53` mostra o
      `focusguard-daemon.exe` em UDP (e TCP).
- [ ] `focusguard dns status` (na pasta de instalação, via PowerShell admin)
      mostra listening + upstream padrão `1.1.1.2:53`.
- [ ] Do mesmo PC: `nslookup focusguard.com 127.0.0.1` responde (sinkhole
      encaminha consultas liberadas ao upstream).
- [ ] Do celular (com o DNS do roteador apontado para este PC): site de
      distração bloqueado não abre; site normal abre.

### 1.4 Reinício (flag persistido manda)

- [ ] Reiniciar a máquina: o DNS volta a subir sozinho (agora via `dns_enabled`
      persistido no `state.json`, não mais pelo marcador).
- [ ] `focusguard dns stop` → reinício → DNS continua desligado (persistência).

---

## 🔑 2. Login admin na interface web

### 2.1 Primeiro acesso

- [ ] Abrir `http://127.0.0.1:48902` → splash → tela de login.
- [ ] Entrar com usuário **`admin`** e a **senha padrão da instalação**
      (constante no código; troque no primeiro login).
- [ ] Login com senha errada: mensagem única "usuário ou senha inválidos".
- [ ] 5 tentativas erradas seguidas → **429** ("muitas tentativas — aguarde 30s").
- [ ] Logout (botão na sidebar) → volta para a tela de login; reabrir a UI sem
      sessão mostra o login.

### 2.2 Troca de senha + sessão

- [ ] Configurações → **Minha conta** → trocar a senha do `admin` (com
      confirmação). A sessão atual continua válida.
- [ ] Deslogar e reentrar com a **nova** senha (a antiga não funciona mais).

### 2.3 Gestão de usuários (admin)

- [ ] Configurações → **Usuários** lista `admin`.
- [ ] Criar usuário comum (ex.: `maria`, senha ≥ 8) → aparece na lista.
- [ ] Entrar como `maria`: card vira **"Minha conta"** (só troca a própria
      senha); **não** vê a gestão de usuários.
- [ ] Como `maria`, tentar trocar a senha de outro usuário via API direta →
      403 ("você só pode alterar a própria senha").
- [ ] Como `admin`, trocar a senha de `maria` e remover `maria`.
- [ ] O `admin` **não** tem botão de remover (o daemon também rejeita).

### 2.4 Reset de senha (caminho de recuperação)

- [ ] Parar o serviço (`sc stop FocusGuard`), apagar
      `%PROGRAMDATA%\FocusGuard\user.json`, iniciar o serviço → o boot
      re-seeda o `admin` com a senha padrão.
- [ ] Confirmar que `user.json` (recriado) contém **apenas hashes bcrypt**
      (nunca senha em texto puro).

---

## 🌐 3. Relacionados da v0.15.0 (sanidade rápida)

- [ ] Tela **Rede**: toggle Ligar/Desligar; chips de upstream (Cloudflare,
      Google, Quad9, AdGuard) + campo custom; trocar upstream reinicia o
      sinkhole e zera os contadores (aviso na tela); `dns_upstream` persiste
      após reinício do PC.
- [ ] **MSI desktop** (`focusguard-0.15.0-amd64.msi`) numa segunda máquina:
      instala com tray + atalho desktop + chave Run; DNS **desligado** por
      padrão (sem marcador).
- [ ] **Troca de sabor** (mesmo UpgradeCode): instalar o MSI desktop sobre o
      Server → vira desktop (tray volta, `server.role` some, DNS segue o flag
      persistido — provavelmente **ligado**, já que o `state.json` foi
      mantido). Registrar o comportamento observado.

---

## ✅ 4. Critérios de aceite (resumo)

- [ ] Instalação limpa do Server → DNS ativo no 1º boot, sem nenhuma ação.
- [ ] Login admin funciona; primeiro acesso documentado; senha trocada.
- [ ] Gestão de usuários e permissões funcionam (admin vs comum).
- [ ] Nenhum regressão no MSI desktop e no fluxo de atualização.

---

## 🧹 5. Observações / achados

> Preencher aqui qualquer divergência encontrada (ex.: porta 53 ocupada pelo
> ICS do Windows, atalho do Menu Iniciar sem ícone, mensagens de erro, etc.)
> para virar issue/commit da próxima versão.
