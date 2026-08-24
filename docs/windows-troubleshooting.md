# Troubleshooting: Falsos Positivos no Windows

> Guia para resolver bloqueios indevidos do Windows Defender, SmartScreen e
> outros antivírus ao instalar ou executar o FocusGuard.

---

## Por que o Windows bloqueia o FocusGuard?

O FocusGuard é um software legítimo de bloqueio focado de distrações que, por
natureza, precisa modificar configurações do sistema para funcionar:

| Comportamento | Por que é necessário | Por que antivírus bloqueia |
|---------------|---------------------|---------------------------|
| Modifica `hosts` | Bloqueia sites indesejados | Comportamento típico de malware |
| Cria regras de firewall | Bloqueia IPs específicos | Pode ser usado para bloquear segurança |
| Escuta na porta 53 (DNS) | DNS sinkhole para rede | Interfere com DNS do sistema |
| Instala serviço Windows | Roda em background | Serviços são alvos de análise |
| Executa como administrador | Necessário para modificar o sistema | Privilégios elevados são suspeitos |
| Auto-atualização | Mantém software atualizado | Substituir binários é incomum |

> ⚠️ **Importante:** Todos esses comportamentos são **intencionais e
> necessários** para que o FocusGuard funcione. O software não contém
> malware, spyware ou qualquer código malicioso.

---

## 1. Windows SmartScreen

### Sintoma

Ao executar o `.exe` ou `.msi`, o Windows exibe:

```
⚠ Windows protegeu seu computador
O Microsoft Defender SmartScreen impediu a inicialização de um aplicativo não reconhecido.
Executar esse aplicativo pode colocar seu computador em risco.
```

### Solução

**Se você baixou de fonte oficial (GitHub Releases):**

1. Clique em **"Mais informações"**
2. Clique em **"Executar assim mesmo"**
3. O software será executado normalmente

**Para evitar o aviso permanentemente:**

1. Clique com o botão direito no arquivo `.exe` ou `.msi`
2. Selecione **"Propriedades"**
3. Na parte inferior, marque **"Desbloquear"** (se disponível)
4. Clique em **"Aplicar"** → **"OK"**
5. Agora execute normalmente

**Para desativar o SmartScreen (não recomendado):**

1. Abra **Configurações** → **Privacidade e segurança** → **Segurança do Windows**
2. Clique em **"Proteção de vírus e ameaças"**
3. Role até **"Proteção contra vírus e ameaças"**
4. Clique em **"Gerenciar configurações"** (em "Configurações de proteção contra vírus e ameaças")
5. Desative **"Proteção fornecida pela nuvem"** (temporariamente)

> ⚠️ **Não recomendado:** Desativar o SmartScreen reduz sua segurança. Use
> apenas temporariamente para instalar o FocusGuard.

---

## 2. Windows Defender (Antivírus)

### Sintoma

O Windows Defender bloqueia a instalação ou execução do FocusGuard:

```
⚠ Threats found
Microsoft Defender Antivirus found threats that weren't blocked.
```

Ou o executável é deletado automaticamente.

### Solução

#### Opção 1: Excluir da verificação (recomendado)

1. Abra o **Windows Security** (Segurança do Windows)
2. Vá em **"Proteção contra vírus e ameaças"**
3. Clique em **"Gerenciar configurações"** em "Configurações de proteção contra vírus e ameaças"
4. Em "Exclusões", clique em **"Adicionar ou remover exclusões"**
5. Clique em **"Adicionar uma exclusão"** e selecione:
   - **Pasta:** `C:\Program Files\FocusGuard`
   - **Processo:** `focusguard-daemon.exe`
   - **Tipo de arquivo:** `.exe` (opcional, mais amplo)

#### Opção 2: Restaurar arquivo bloqueado

1. Abra o **Windows Security**
2. Vá em **"Proteção contra vírus e ameaças"**
3. Clique em **"Histórico de proteção"**
4. Encontre o item bloqueado (FocusGuard)
5. Clique em **"Ações"** → **"Permitir no dispositivo"**

#### Opção 3: Desativar temporariamente

1. Abra o **Windows Security**
2. Vá em **"Proteção contra vírus e ameaças"**
3. Clique em **"Gerenciar configurações"**
4. Desative **"Proteção em tempo real"** (temporariamente)
5. Instale o FocusGuard
6. **Reative imediatamente** a proteção em tempo real

> ⚠️ **Nunca deixe a proteção em tempo real desativada.** Reative após
> a instalação.

---

## 3. Outros Antivírus

### Norton

1. Abra o Norton
2. Vá em **"Settings"** → **"Firewall"**
3. Em **"Program Control"**, encontre o FocusGuard
4. Defina como **"Allow"**
5. Ou adicione uma exclusão em **"Settings"** → **"Antivirus"** → **"Exclusions"**

### Kaspersky

1. Abra o Kaspersky
2. Vá em **"Settings"** → **"Protection"**
3. Clique em **"Exclusions and types of objects to skip"**
4. Adicione a pasta `C:\Program Files\FocusGuard`
5. Clique em **"Add"** → **"OK"**

### Bitdefender

1. Abra o Bitdefender
2. Vá em **"Protection"** → **"Antivirus"**
3. Clique em **"Settings"** (engrenagem)
4. Vá em **"Exclusions"**
5. Adicione a pasta `C:\Program Files\FocusGuard`

### Avast/AVG

1. Abra o Avast/AVG
2. Vá em **"Menu"** → **"Settings"**
3. Vá em **"General"** → **"Exclusions"**
4. Adicione a pasta `C:\Program Files\FocusGuard`

### ESET

1. Abra o ESET
2. Vá em **"Setup"** → **"Computer Protection"**
3. Clique em **"Detection Engine"** → **"Exclusions"**
4. Adicione a pasta `C:\Program Files\FocusGuard`

---

## 4. Verificar Assinatura Digital

### Por que verificar?

O FocusGuard é distribuído com assinatura digital Authenticode (a partir da
versão 0.21.0). A assinatura garante que:

- O software não foi modificado desde que foi assinado
- O publisher é verificado (FocusGuard)
- O software é confiável e seguro

### Como verificar

#### Opção 1: Propriedades do arquivo

1. Clique com o botão direito no `focusguard-daemon.exe`
2. Selecione **"Propriedades"**
3. Vá na aba **"Geral"**
4. Deve aparecer **"Este arquivo foi obtido de outra pessoa e pode ser
   perigoso"** — isso é normal para downloads da internet
5. Clique em **"Desbloquear"** (se disponível) → **"Aplicar"** → **"OK"**

#### Opção 2: PowerShell

```powershell
Get-AuthenticodeSignature "C:\Program Files\FocusGuard\focusguard-daemon.exe"
```

Resultado esperado (com assinatura válida):
```
Status        : Valid
SignerCertificate : [Subject]
                     CN=FocusGuard, O=FocusGuard, L=...
```

#### Opção 3: Signtool (Windows SDK)

```powershell
signtool verify /pa "C:\Program Files\FocusGuard\focusguard-daemon.exe"
```

Resultado esperado:
```
Successfully verified: C:\Program Files\FocusGuard\focusguard-daemon.exe
```

---

## 5. Erros Comuns de Instalação

### "Acesso negado" ao executar install-daemon.ps1

**Causa:** O PowerShell não está elevado (Administrador).

**Solução:**
1. Clique com o botão direito no **PowerShell**
2. Selecione **"Executar como administrador"**
3. Execute novamente:
   ```powershell
   Set-ExecutionPolicy -Scope Process Bypass
   .\install-daemon.ps1 install
   ```

### Serviço não inicia

**Causa:** O executável foi bloqueado ou deletado pelo antivírus.

**Solução:**
1. Verifique se o arquivo existe em `C:\Program Files\FocusGuard\`
2. Se não existir, adicione a exclusão no antivírus (veja seção 2)
3. Reinstale o FocusGuard
4. Verifique o serviço:
   ```powershell
   sc query FocusGuard
   ```

### "O daemon não responde"

**Causa:** O serviço pode estar travado ou o antivírus bloqueou sua execução.

**Solução:**
1. Verifique o status do serviço:
   ```powershell
   sc query FocusGuard
   ```
2. Reinicie o serviço:
   ```powershell
   sc stop FocusGuard
   sc start FocusGuard
   ```
3. Verifique o log do daemon:
   ```powershell
   Get-Content "C:\Program Files\FocusGuard\focusguard-daemon.log" -Tail 50
   ```
4. Se o antivírus bloqueou, adicione a exclusão e reinstale

### SmartScreen bloqueia o .msi

**Causa:** O MSI não está assinado ou o SmartScreen não reconhece o editor.

**Solução:**
1. Clique em **"Mais informações"**
2. Clique em **"Executar assim mesmo"**
3. Ou desbloqueie o arquivo (clique direito → Propriedades → Desbloquear)

---

## 6. Verificar se o FocusGuard está Funcionando

Após resolver o bloqueio, verifique se tudo está funcionando:

### 1. Status do serviço

```powershell
sc query FocusGuard
```

Resultado esperado:
```
STATE              : 4  RUNNING
```

### 2. Status do FocusGuard

```powershell
focusguard status
```

Deve mostrar o estado da proteção (bloqueios ativos, DNS, etc.)

### 3. Teste de bloqueio

```powershell
focusguard block facebook.com --duration 5m
```

Acesse `facebook.com` no navegador — deve aparecer a página de bloqueio do
FocusGuard.

### 4. Verificar log

```powershell
Get-Content "C:\Program Files\FocusGuard\focusguard-daemon.log" -Tail 20
```

Não deve haver erros de "access denied" ou "blocked by antivirus".

---

## 7. Contato e Suporte

Se nenhum passo acima resolver:

1. **Abra uma issue** no GitHub: https://github.com/focusguard/focusguard/issues
2. **Inclua:**
   - Versão do FocusGuard
   - Versão do Windows
   - Nome do antivírus (se não for o Defender)
   - Mensagem de erro exata
   - Print da tela de bloqueio (se houver)

3. **Log do daemon** (se disponível):
   ```powershell
   Get-Content "C:\Program Files\FocusGuard\focusguard-daemon.log" -Tail 100 > debug.log
   ```

---

## 8. Perguntas Frequentes

### O FocusGuard é seguro?

Sim. O FocusGuard é um software open-source que:
- Modifica apenas arquivos de configuração do sistema (hosts, firewall)
- Não envia dados para servidores externos
- Não contém código malicioso
- É verificável pelo código-fonte no GitHub

### Por que o antivírus bloqueia se é seguro?

Antivíus usam heurísticas e comportamento para detectar ameaças. Como o
FocusGuard:
- Modifica o arquivo `hosts` (comportamento de malware)
- Instala serviços (comportamento de malware)
- Escuta em portas de rede (comportamento de malware)

Ele aciona essas heurísticas. É um **falso positivo** — o software é legítimo,
mas seu comportamento se assemelha ao de malware.

### Posso desativar o antivírus permanentemente?

**Não recomendado.** O antivírus protege contra ameaças reais. Use apenas
exclusões específicas para o FocusGuard.

### O FocusGuard coleta dados?

Não. O FocusGuard roda localmente e não envia dados para servidores externos.
A única conexão de rede é para atualizações (via GitHub Releases) e DNS
upstream (Cloudflare Security, 1.1.1.2).

### Como sei se a versão é autêntica?

1. Baixe sempre de fontes oficiais (GitHub Releases)
2. Verifique a assinatura digital (veja seção 4)
3. Compare o checksum com o publicado na release:
   ```powershell
   Get-FileHash focusguard-daemon.exe -Algorithm SHA256
   ```

---

## 9. Referências

- [Microsoft: Proteger o PC do Windows](https://support.microsoft.com/pt-br/windows/proteger-seu-pc-do-windows-10-2b090f8d-2d15-4c23-8832-01f24c7aef67)
- [Windows Defender: Adicionar exclusões](https://support.microsoft.com/pt-br/windows/adicionar-exclusoes-no-microsoft-defender-antivirus-7e2b8dc9-a9b0-48c4-868b-28f294ba14b2)
- [SmartScreen: O que é e como funciona](https://support.microsoft.com/pt-br/windows/o-que-e-o-microsoft-defender-smartscreen-e-como-ele-pode-me-proteger-3d4df055-c4a4-4b9b-a2d2-97d43e4b5b75)
- [Autenticode: Assinatura digital](https://learn.microsoft.com/pt-br/windows/win32/secauthn/authenticode-portal)
