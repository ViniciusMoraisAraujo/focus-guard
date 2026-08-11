# FocusGuard 🛡️

**Bloqueie sites e apps que roubam sua atenção — na raiz, no nível do sistema.**

O FocusGuard bloqueia no **sistema inteiro** (arquivo `hosts` + firewall), não
só no navegador: o site não abre em nenhum browser nem app da máquina — e, na
edição **Server**, em qualquer dispositivo da rede. Os bloqueios **expiriam
sozinhos** e **não podem ser desfeitos antes da hora** — nada de "só mais
cinco minutos".

Você controla tudo pelo **painel web** e pela **bandeja do sistema** — sem
abrir terminal.

---

## ✨ Funcionalidades

- 🖥️ **Painel web completo** — bloquear, pomodoro, agenda, estatísticas, segurança e configurações em um só lugar
- 🔔 **Bandeja do sistema** — bloco rápido de 4h com um clique, status e atualização
- 🚫 **Bloqueio de sites** — por domínio, por categoria (`social`, `video`, `news`, `games`) ou **toda a internet** (modo pânico, com allowlist)
- ⏳ **Temporário por natureza** — expira sozinho; nada de desbloqueio antecipado
- 🍅 **Pomodoro com categorias** — ciclos de trabalho/descanso, modo estrito e sessões nomeadas (missões)
- ⏰ **Agenda recorrente** — bloqueia em dias e horários fixos, com importação de calendário (.ics)
- 🎯 **Metas, estatísticas e conquistas** — streak diário, gráficos, relatórios e badges
- 🛡️ **À prova de burla** — edições no `hosts`/estado são detectadas (SHA-256) e revertidas automaticamente; mexer no relógio do sistema também é pego
- 🌍 **DNS sinkhole (edição Server)** — bloqueia para a rede inteira: celular, TV, console

---

## 🚀 Comece agora

**1. Instale**

| Sistema | Como |
|---|---|
| 🪟 Windows | Baixe o `.msi` da [release](https://github.com/ViniciusMoraisAraujo/focus-guard/releases) e execute |
| 🐧 Linux | `sudo ./install-linux.sh install` (dentro do `.tar.gz` da release) |

**2. Abra o painel**

Clique no atalho **FocusGuard** criado no Desktop (ou na bandeja →
**🌐 Abrir painel**). O painel abre no navegador em
`http://127.0.0.1:48902`. No **primeiro acesso**, entre com `admin` /
`SP02cfasm#` e **troque a senha** em Configurações.

**3. Bloqueie algo**

- **Painel** — aba *Bloquear*: digite o site, escolha a duração e confirme;
- **Bandeja** — *🚫 Bloco rápido*: um clique no site (4h).

Pronto. 🎯

<details>
<summary><b>📦 Instalação detalhada & desinstalação</b></summary>

**Windows** — o instalador `.msi` faz tudo: instala os 5 executáveis em
`C:\Program Files\FocusGuard\`, cria os serviços `FocusGuard` (daemon) e
`FocusGuardWatchdog` com recuperação automática, registra a bandeja e cria o
atalho no Desktop. Desinstale em *Programas e Recursos* (`msiexec /x …`); o
estado em `C:\ProgramData\FocusGuard\` é preservado.

**Linux** — `install-linux.sh` instala em `/opt/focusguard/`, cria a unit
systemd, o grupo `focusguard`, a bandeja (autostart) e o atalho no Desktop.
Para o painel/bandeja funcionarem **sem sudo**, seu usuário precisa estar no
grupo:

```bash
sudo usermod -aG focusguard $USER    # e faça logout/login
```

**Onde ficam os arquivos**

| | Binários (pasta protegida) | Dados |
|---|---|---|
| 🐧 Linux | `/opt/focusguard/` | `/var/lib/focusguard/` |
| 🪟 Windows | `C:\Program Files\FocusGuard\` | `C:\ProgramData\FocusGuard\` |

**Desinstalar** — Windows: *Programas e Recursos* (ou `msiexec /x …`).
Linux: `sudo ./install-linux.sh uninstall`.

> ⚠️ Instalar e gerenciar o serviço exige **Administrador** (Windows) ou
> **sudo** (Linux). O uso normal não.
</details>

---

## 🖥️ Usando o painel

| Tela | O que faz |
|---|---|
| **Dashboard** | Status da proteção, bloqueios ativos com countdown, meta do dia |
| **Bloquear** | Bloqueia um site ou categoria por um tempo |
| **Pânico** | Corta toda a internet de uma vez (com allowlist opcional) |
| **Pomodoro** | Sessões de foco com ciclos de trabalho/descanso e missões |
| **Agenda** | Regras recorrentes por dia/horário + importação de calendário (.ics) |
| **Apps** | Escolhe quais apps são encerrados durante o foco (ex: Spotify, Steam) |
| **Presets** | Cria categorias personalizadas de sites |
| **Estatísticas** | Gráficos de foco, streak, missões e exportação de relatórios |
| **Rede** | DNS sinkhole e página de bloqueio (edição Server) |
| **Segurança** | Histórico de tentativas de burla e eventos de relógio |
| **Configurações** | Meta diária, senha/usuários, canal de atualizações |

---

## 🔔 Bandeja do sistema

| Item | Ação |
|------|------|
| 📊 Status | Atualiza o tooltip com proteção, regras e bloqueios ativos |
| 🚫 Bloco rápido | Bloqueia um site por **4h** (`youtube`, `twitter`, `instagram`, `tiktok`, `reddit`, `netflix`) |
| 🗂 Categorias | Bloqueia uma categoria (preset) por 4h |
| 🔄 Verificar atualização | Aplica a nova versão na hora |
| 🌐 Abrir painel | Abre a interface web |
| 🚪 Sair | Fecha a bandeja (o daemon continua rodando) |

A bandeja também notifica quando há nova versão e nas transições do pomodoro
(trabalho → descanso → trabalho).

---

## 🛡️ Segurança por baixo dos panos

- **Dupla camada** — `hosts` + firewall (IPv4 e IPv6). Remover uma não libera o site.
- **Anti-burla** — o `hosts` e o estado são monitorados em tempo real: edição externa é detectada (SHA-256) e **revertida automaticamente**; o histórico fica na tela *Segurança*.
- **Clock guard** — mexer no relógio do sistema para encurtar bloqueios é detectado via NTP e gera bloqueio preventivo.
- **Smart Recovery** — se uma atualização quebrar o daemon, o watchdog restaura a versão anterior automaticamente.
- **Bloqueios que persistem** — sobrevivem a reinicializações e são reaplicados no boot.

## 🔄 Atualizações

O daemon verifica novas versões automaticamente a cada 24h; a bandeja avisa e
**🔄 Verificar atualização** aplica na hora. A suíte inteira é atualizada de
uma vez e o daemon reinicia sozinho; se um executável estiver em uso, a troca
é agendada para o **próximo reinício** (o painel/bandeja avisam). Cópias
antigas e backups expirados são removidos sozinhos.

## ❓ Dúvidas frequentes

**O site bloqueado continua abrindo**
Limpe o cache DNS (`ipconfig /flushdns` no Windows). Navegadores com DoH/QUIC
próprios são cobertos na porta 853; QUIC/DoH3 (UDP 443) ainda não é bloqueado
automaticamente.

**Como desbloquear antes da hora?**
Não dá — é de propósito. O bloqueio expira sozinho; não é possível
desbloquear antes da hora. Sessão pomodoro não-estrita pode ser encerrada no
painel.

**O bloqueio volta depois de reiniciar o PC?**
Sim — os bloqueios ativos são persistidos e reaplicados no boot.

**O painel não abre**
Feche e reabra pelo atalho do Desktop ou bandeja. O log do servidor web
(`focusguard-web.log`) fica ao lado do executável, ou em
`C:\ProgramData\FocusGuard\` / `/var/lib/focusguard/`.

**"Porta 53 em uso" ao ligar o DNS sinkhole (Windows)**
O culpado quase sempre é o ICS: `sc config SharedAccess start= disabled` +
`net stop SharedAccess` (como Administrador).

**Preciso de Administrador para tudo?**
Não — só para instalar/gerenciar o serviço e aplicar regras de sistema.

<details>
<summary><b>🌍 Edição Server — DNS sinkhole ("Rei da Rede")</b></summary>

Na edição **Server** (instalador `focusguard-server-*.msi`), o daemon vira um
servidor DNS na porta 53: responde `0.0.0.0` para sites bloqueados e
encaminha o resto ao upstream Cloudflare Security (`1.1.1.2`, com filtro de
malware). **Todos os dispositivos da rede** que usarem o DNS desta máquina
ficam protegidos — celular, TV, console. Ative pela tela **Rede** do painel.

**No roteador:** ① reserve um IP fixo para o PC do FocusGuard; ② aponte o DNS
primário do DHCP para esse IP; ③ deixe um DNS público (ex: `1.1.1.1`) como
secundário, para a rede seguir navegando se o PC cair.

Recursos extras da edição Server: **políticas por dispositivo** (regras
diferentes por IP na rede, pela tela *Rede*) e **página de bloqueio** nos
sites (HTTP :80 e HTTPS :443), para o usuário ver *por que* o site não abre.
</details>

---

## 📜 Licença

MIT.

---

## 🛠️ Desenvolvedor?

Quer compilar, testar ou contribuir? Veja o
[**guia de desenvolvimento**](docs/development.md) — build, arquitetura, testes
e processo de release.

> **FocusGuard** — Protegendo seu foco, um bloqueio de cada vez. 🎯
