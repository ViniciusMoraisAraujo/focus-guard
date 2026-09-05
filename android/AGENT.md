# AGENT.md — android/

> Guia para agentes de IA que trabalham neste diretório. Consulte também o
> **[AGENT.md](../AGENT.md)** na raiz (specs, convenções, arquitetura) — leia-o
> antes de editar qualquer código.

## Propósito

Aplicativo móvel nativo **Android (Kotlin + Jetpack Compose + Material 3)** do FocusGuard.
Fornece um bloqueador de distrações independente e de alta integridade diretamente no smartphone, sem exigir root:
- **Bloqueio de Sites**: Interceptação local de consultas DNS via `android.net.VpnService` (sinkhole local em `0.0.0.0`, sem proxy externo e sem latência).
- **Bloqueio de Apps**: Detecção de aplicativos proibidos em primeiro plano via `android.accessibilityservice.AccessibilityService`.
- **Tela de Interceptor (Overlay)**: Interface focada com contagem regressiva, mensagem motivacional e exercício de respiração (*Box Breathing* 4-4-4-4) cobrindo apps proibidos via `TYPE_APPLICATION_OVERLAY`.
- **Anti-Burla de Sessão Ativa**: Durante uma sessão de bloqueio, é proibido pausar ou encerrar o app; desinstalação é permitida apenas em períodos ociosos (idle).

---

## Estrutura

| Caminho | Papel |
|---|---|
| `build.gradle.kts` | Configuração raiz do Gradle (plugins Android Application, Kotlin Android e Compose Compiler) |
| `settings.gradle.kts` | Repositórios centralizados (Google, Maven Central, Gradle Plugin Portal) e inclusão do módulo `:app` |
| `gradle.properties` | Configurações da JVM do Gradle e flags AndroidX |
| `gradle/libs.versions.toml` | **Version Catalog**: versões centralizadas de plugins e bibliotecas (AGP 8.4.2, Kotlin 2.0.0, Compose BOM) |
| `gradle/wrapper/` | Gradle Wrapper 8.7 (`gradle-wrapper.properties` + `.jar`) |
| `gradlew` / `gradlew.bat` | Scripts executáveis do Gradle Wrapper para Linux, macOS e Windows |
| `app/build.gradle.kts` | Configuração do módulo Android: `compileSdk 34`, `minSdk 26`, dependências Compose e Material 3 |
| `app/proguard-rules.pro` | Regras de ofuscação/otimização ProGuard/R8 |
| `app/src/main/AndroidManifest.xml` | Declaração de componentes, atividades e permissões (`VpnService`, `AccessibilityService`, `Overlay`, `ForegroundService`) |
| `app/src/main/java/com/focusguard/app/MainActivity.kt` | Ponto de entrada e dashboard principal da interface Compose |
| `app/src/main/java/com/focusguard/app/ui/theme/` | Design tokens do FocusGuard: `Color.kt` (Dark Slate & Emerald), `Theme.kt`, `Type.kt` |
| `app/src/main/res/` | Recursos Android: strings, cores base, temas XML e ícones adaptativos (`mipmap-anydpi-v26`) |
| `README.md` | Guia de execução no Android Studio e compilação via CLI |

---

## Regras específicas

1. **Metodologia TDD Obrigatória (Testes Primeiro)**:
   - Todo desenvolvimento (lógica de bloqueio, scheduler, parser DNS, timers ou correção de bugs) DEVE seguir estritamente o ciclo **TDD (Test-Driven Development)**:
     1. **Red**: Escreva primeiro o teste unitário/instrumentado que descreve o comportamento esperado e veja-o falhar.
     2. **Green**: Implemente o código mínimo necessário para fazer o teste passar.
     3. **Refactor**: Limpe o design e elimine redundâncias com os testes verdes garantindo não regressão.
   - Jamais implemente código de produção sem o respectivo teste escrito previamente.
2. **Zero Root**: O app opera estritamente no espaço de usuário do Android. Não tente ler `/etc/hosts` nem invocar `iptables` via shell — no Android sem root isso é bloqueado pelo SELinux.
3. **Bloqueio de Rede via `VpnService` Local**:
   - O `VpnService` cria uma interface virtual local (`10.0.0.1`).
   - O serviço **não** roteia dados para servidores externos; ele apenas filtra pacotes UDP/TCP na porta 53 (DNS).
   - Domínios permitidos são repassados ao resolver upstream padrão (ex: `1.1.1.1` ou o DNS da rede móvel/Wi-Fi). Domínios bloqueados recebem resposta sinkhole (`0.0.0.0`).
4. **Bloqueio de Apps via `AccessibilityService`**:
   - O serviço de acessibilidade escuta eventos `TYPE_WINDOW_STATE_CHANGED`.
   - Ao detectar um pacote na lista de bloqueio (ex: `com.instagram.android`, `com.zhiliaoapp.musically`), aciona imediatamente a sobreposição da tela de Interceptor ou redireciona para a Home.
5. **Tela de Interceptor Overlay**:
   - Utiliza `WindowManager.LayoutParams.TYPE_APPLICATION_OVERLAY` para cobrir o app proibido e impedir interação física.
   - Deve replicar a identidade visual do FocusGuard Desktop: contagem regressiva, exercício de respiração e mensagem de foco.
6. **Anti-Burla de Sessão Ativa**:
   - Durante uma sessão de bloqueio em andamento, o app **não deve oferecer botão de desligar, pausar ou cancelar**.
   - Se o usuário tentar acessar a tela de desinstalação nas configurações do sistema enquanto um bloqueio estiver ativo, a acessibilidade deve interceptar e redirecionar para a Home.
   - Desinstalação e desativação são livres quando o app estiver em estado ocioso (idle, sem timers rodando).
7. **Tempo Seguro Contra Fraudes de Relógio**:
   - Nunca use `System.currentTimeMillis()` para verificar término de timers (o usuário pode adiantar a hora nas Configurações).
   - Use sempre `SystemClock.elapsedRealtime()` (hardware ticks desde o boot do aparelho) para calcular a expiração de timers no Android.
8. **Ciclo de Vida e Segundo Plano**:
   - Qualquer bloqueio em execução deve estar atrelado a um `ForegroundService` com notificação persistente no painel do Android.
   - Isso impede que o sistema operacional finalize o serviço por pressão de memória (OOM Killer) ou economia de bateria.
9. **Design System**:
   - Cores e tema escuro idênticos ao desktop: Fundo `#090A0F`, Cards `#12151F`, Destaque Esmeralda `#10B981`, Alerta `#EF4444`.
10. **Resumo de Sessão**:
   - Ao final da sessão, atualize o `../docs/session-log/YYYY-MM-DD.md` (handoff diário para o próximo agente — regra do AGENT.md raiz §4.15).

---

## Armadilhas conhecidas (Gotchas do Android)

- **Android 14 (API 34) Foreground Service Types**:
  O Android 14 exige declarar explicitamente o atributo `android:foregroundServiceType` para qualquer serviço em primeiro plano no manifesto (ex.: `specialUse` ou `systemExempted`). Omitir isso causa `SecurityException` em tempo de execução ao chamar `startForeground()`.
- **Doze Mode e Otimização de Bateria das Fabricantes**:
  Algumas fabricantes (Samsung, Xiaomi, Huawei) congelam agressivamente sockets de VPN quando a tela é desligada. O app deve solicitar ao usuário a isenção de otimização de bateria (`Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS`).
- **Conflito de VPNs**:
  O Android permite apenas uma VPN ativa por vez no sistema operacional. Se o usuário ativar outra VPN (ex: Cloudflare Warp, NordVPN), o Android derruba o `VpnService` do FocusGuard. O app deve monitorar esse evento e notificar o usuário imediatamente.
- **Serviço de Acessibilidade Desligado pelo SO**:
  Em alguns aparelhos com pouca RAM ou após reiniciar o telefone, o serviço de acessibilidade pode ficar pausado. A `MainActivity` deve sempre checar se as permissões continuam ativas no `onResume()` e exibir o status ao usuário.

---

## Validação

- **Compilação do APK de Debug**:
  ```bash
  cd android
  ./gradlew assembleDebug
  ```
- **Execução dos Testes Unitários**:
  ```bash
  cd android
  ./gradlew test
  ```
- **Verificação de Lint e Boas Práticas**:
  ```bash
  cd android
  ./gradlew lint
  ```
