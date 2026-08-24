# Plano: Prevenir bloqueio do FocusGuard pelo Windows (Defender/SmartScreen)

> **Objetivo:** Analisar hipóteses de por que o Windows pode bloquear o
> FocusGuard e definir um plano de ação para minimizar falsos positivos.

---

## 1. Contexto

O FocusGuard é uma aplicação Go que:
- Modifica o arquivo `hosts` do sistema (`C:\Windows\System32\drivers\etc\hosts`)
- Cria e gerencia regras de firewall via `netsh advfirewall`
- Executa como serviço Windows com privilégios de administrador (`requireAdministrator`)
- Escuta na porta 53 (DNS sinkhole)
- Interrompe processos (`processguard`)
- Faz auto-atualização (`go-selfupdate`)
- Registra-se na inicialização (`HKCU\...\Run`)

Todas essas comportamentos são legítimos para um software de bloqueio focado, mas
podem ser interpretados como maliciosos por antivírus e SmartScreen.

---

## 2. Hipóteses de Bloqueio

### H1: Binários sem assinatura digital (Code Signing)
**Severidade:** ⚠️ ALTA
**Evidência:** Os binários não possuem assinatura digital (Authenticode). O
SmartScreen do Windows e muitos antivírus bloqueiam executáveis não assinados
baixados da internet, especialmente quando solicitam privilégios elevados.

**Impacto:**
- Windows SmartScreen exibe aviso "Windows protegeu seu computador" ao executar
- Usuários menos experientes não conseguem contornar o aviso
- Alguns antivírus bloqueiam silenciosamente executáveis não assinados de fontes
  desconhecidas

### H2: Comportamento de modificações do sistema
**Severidade:** ⚠️ ALTA
**Evidência:** O daemon modifica arquivos e configurações críticas do sistema:
- Edita `hosts` (comportamento típico de malware para redirecionar tráfego)
- Adiciona regras de firewall (pode ser usado para bloquear comunicações de segurança)
- Escuta na porta 53 (pode interferir com o DNS do sistema)
- Instala serviços Windows

**Impacto:**
- Windows Defender pode bloquear com base em heurísticas comportamentais
- Google Safe Browsing pode marcar o software como perigoso
- Outros antivírus podem classificar como "Potentially Unwanted Program" (PUP)

### H3: Ausência de reputação digital
**Severidade:** ⚠️ MÉDIA
**Evidência:** O software não está assinado e não possui reputation no Microsoft
Defender SmartScreen. Software desconhecido sem assinatura digital recebe
reputação zero.

**Impacto:**
- SmartScreen bloqueia por falta de reputação mesmo sem assinatura
- O bloqueio é mais agressivo para executáveis que solicitam elevation

### H4: Tráfego de download do GitHub
**Severidade:** ⚠️ MÉDIA
**Evidência:** O software é distribuído via GitHub Releases. Downloads do GitHub
podem ser marcados por algumas ferramentas de segurança por serem considerados
"fontes não verificadas".

**Impacto:**
- O Mark of the Web (MOTW) é aplicado ao download
- SmartScreen usa o MOTW para decidir sobre bloqueio
- Executáveis com MOTW e sem assinatura são bloqueados mais agressivamente

### H5: Auto-atualização via `go-selfupdate`
**Severidade:** ⚠️ BAIXA
**Evidência:** O mecanismo de auto-atualização baixa e substitui binários em
tempo de execução, o que pode ser interpretado como comportamento malicioso.

**Impacto:**
- Alguns antivírus bloqueiam processos que substituem seus próprios binários
- Pode ativar alertas de "behavior monitoring"

### H6: Modificação de certificados de confiança (CA local)
**Severidade:** ⚠️ BAIXA
**Evidência:** O FocusGuard cria uma CA local e a instala no trust store do
sistema (`certutil -addstore Root`).

**Impacto:**
- Instalar uma CA no trust store é comportamento suspeito
- Pode ser detectado por ferramentas de segurança que monitoram o cert store

---

## 3. Soluções Propostas

### 3.1 Code Signing (PRIORIDADE MÁXIMA)

**Ação:** Obter um certificado Authenticode para assinar os binários Windows.

**Opções de provedores:**
| Provedor | Preço (ano) | Validação | Suporte EV |
|----------|-------------|-----------|------------|
| Sectigo | ~$70-80 | Organization (OV) | Sim |
| DigiCert | ~$200+ | Organization (OV) | Sim |
| GlobalSign | ~$200+ | Organization (OV) | Sim |
| Certum | ~$50-60 | Individual/Organization | Não |

**Recomendação:** Certum (mais acessível) ou Sectigo (reconhecimento broader).

**Implementação:**
1. Adicionar job `sign` no `.github/workflows/release.yml` após o GoReleaser
2. Usar `signtool.exe` do Windows SDK para assinar cada binário
3. Armazenar o certificado como GitHub Secret (encrypted PFX)
4. Assinar com timestamp (`/tr http://timestamp.digicert.com`)

**Estrutura do job de assinatura:**
```yaml
sign-windows:
  runs-on: windows-latest
  needs: goreleaser
  steps:
    - name: Download release binaries
      # Download dos .exe da release
    - name: Import certificate
      # Importar PFX do secret
    - name: Sign binaries
      # signtool sign /f cert.pfx /p $PASSWORD /tr timestamp_url ...
    - name: Re-upload signed binaries
      # Substituir binários na release
```

### 3.2 SmartScreen Reputation

**Ação:** Solicitar reputation no Microsoft Partner Center.

**Requisitos:**
- Certificado de assinatura válido (OV ou EV)
- Submeter o binário para análise
- Construir reputation gradualmente (downloads → reclamações → aprovação)

**Timeline:** 2-4 semanas após assinatura.

### 3.3 Mitigações Comportamentais

**Ação:** Reduzir comportamentos que disparam heurísticas.

#### 3.3.1 Manifesto do Daemon
- Manter `requireAdministrator` apenas no daemon (já implementado)
- Adicionar `<requestedExecutionLevel level="asInvoker" uiAccess="false" />`
  para CLI, tray e watchdog (já implementado)

#### 3.3.2 Descrições nos Recursos Windows
- Adicionar `CompanyName`, `FileDescription`, `LegalCopyright` em todos os
  `versioninfo.json` (já implementado)
- Garantir consistência entre `fixed` e `info` versions (já parcialmente
  implementado — há stale versions)

#### 3.3.3 Instalador MSI
- O MSI já é gerado via WiX Toolset, que é reconhecido pelo Windows
- Adicionar certificado de assinatura ao MSI também
- O MSI já registra o produto no "Programs and Features" do Windows

### 3.4 Documentação para o Usuário

**Ação:** Criar guia explicando como contornar falsos positivos.

**Conteúdo:**
1. Como adicionar exceção no Windows Defender
2. Como adicionar exceção no SmartScreen
3. Como importar a CA no Firefox
4. Como verificar a assinatura digital (após implementar)

### 3.5 Submissão a Antivírus

**Ação:** Submeter o software para análise por principais antivírus.

**Lista de antivírus para submissão:**
- Windows Defender (Microsoft Security Intelligence)
- Norton
- McAfee
- Kaspersky
- Bitdefender
- Avast/AVG
- ESET

**URLs de submissão:**
- Microsoft: https://www.microsoft.com/en-us/wdsi/filesubmission
- Norton: https://submit.symantec.com/
- Kaspersky: https://opentip.kaspersky.com/

---

## 4. Plano de Implementação

### Fase 1: Code Signing (1-2 semanas)

**Status:** ✅ CI implementado, pendente obter certificado.

Arquivos criados/modificados:
- `scripts/sign-windows.ps1` — Script de assinatura reutilizável (local + CI)
- `.github/workflows/release.yml` — Job `sign-windows` adicionado

#### Configuração necessária (GitHub Secrets)

| Secret | Descrição |
|--------|----------|
| `WINDOWS_SIGN_CERTIFICATE` | Conteúdo base64 do arquivo `.pfx` |
| `WINDOWS_SIGN_CERTIFICATE_PASSWORD` | Senha do certificado |

#### Como obter o certificado

1. **Escolher provedor:** Sectigo (~$70-80/ano) ou Certum (~$50-60/ano)
2. **Comprar certificado OV** (Organization Validation)
3. **Baixar o .pfx** do painel do provedor
4. **Converter para base64:**
   ```powershell
   [Convert]::ToBase64String([IO.File]::ReadAllBytes("cert.pfx")) | Set-Clipboard
   ```
5. **Adicionar no GitHub:** Settings → Secrets → Actions → Novo secret

#### Fluxo do job `sign-windows`

```
goreleaser (ubuntu) → publica .exe
        ↓
windows-msi (windows) → publica .msi
        ↓
sign-windows (windows) → baixa tudo, assina, re-uploa
```

O job é **condicional**: só roda se `WINDOWS_SIGN_CERTIFICATE` existir.
Se o secret não estiver configurado, a release continua normal (sem assinatura).

#### Como testar localmente

```powershell
# Assinar todos os .exe e .msi em um diretório
.\scripts\sign-windows.ps1 `
  -CertificatePath "C:\path\to\cert.pfx" `
  -CertificatePassword "senha" `
  -SearchPath "bin"

# Verificar se arquivos estão assinados (sem assinar)
.\scripts\sign-windows.ps1 `
  -CertificatePath "C:\path\to\cert.pfx" `
  -CertificatePassword "senha" `
  -SearchPath "bin" `
  -VerifyOnly
```

### Fase 2: Mitigações Comportamentais (1 semana)
- [ ] Atualizar todas as versões stale nos `versioninfo.json`
- [ ] Adicionar `RequestedExecutionLevel asInvoker` para binários não-elevados
- [ ] Testar com Windows Defender em ambiente limpo

### Fase 3: Submissão a Antivírus (2-4 semanas)
- [ ] Submeter para Microsoft (priority)
- [ ] Submeter para top 5 antivírus
- [ ] Documentar resultado de cada submissão

### Fase 4: Documentação (1 semana)
- [ ] Criar `docs/windows-troubleshooting.md`
- [ ] Adicionar seção no README sobre falsos positivos
- [ ] Atualizar `install.txt` com instruções de exceção

### Fase 5: SmartScreen Reputation (2-4 semanas)
- [ ] Registrar no Microsoft Partner Center
- [ ] Submeter binário para análise
- [ ] Monitorar progresso da reputation

---

## 5. Alternativas e Contingências

### Se o Code Signing não for viável financeiramente:
1. **Distribuição via MSI:** O MSI já é mais confiável que .exe solto
2. **Scoop/Winget:** Publicar no winget (requer verificação, mas aumenta confiança)
3. **Documentação robusta:** Guia detalhado para usuários contornarem bloqueios

### Se antivírus continuar bloqueando:
1. **Exclusão temporária:** Orientar usuários a adicionar exceção
2. **Fazer parte do Microsoft Virus Total:** Verificar se binários são detectados
3. **Contato direto:** Alguns provedores de antivírus têm canais para desenvolvedores

---

## 6. Métricas de Sucesso

- ✅ Binários assinados e verificáveis com `signtool verify`
- ✅ SmartScreen não bloqueia após 100+ downloads (reputação)
- ✅ Zero falsos positivos nos top 5 antivírus
- ✅ Usuários conseguem instalar sem intervenção manual

---

## 7. Referências

- [Microsoft Authenticode Signing](https://learn.microsoft.com/en-us/windows/win32/secauthn/authenticode-portal)
- [SmartScreen Filter](https://learn.microsoft.com/en-us/windows/privacy-and-security/windows-defender-smartscreen)
- [WiX Toolset Signing](https://wixtoolset.org/docs/v3/howtos/files_and_registry/signing/)
- [GoReleaser Signing](https://goreleaser.com/customization/signing/)

---

## 8. Perguntas para Decisão

1. **Certificado:** OV (Organization Validation) ou EV (Extended Validation)?
   - OV: ~$70-80/ano, validação mais rápida (3-5 dias)
   - EV: ~$200+/ano, reputação imediata no SmartScreen, validação mais longa (1-2 semanas)

2. **Orçamento:** Qual o limite disponível para certificado + eventuais submissões?

3. **Prioridade:** Focar em code signing primeiro ou paralelizar com documentação?

4. **Distribuição:** Manter .exe + MSI ou migrar para apenas MSI?
