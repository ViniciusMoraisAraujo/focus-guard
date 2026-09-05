# FocusGuard Android 🛡️

Versão móvel nativa do **FocusGuard** para Android, desenvolvida em **Kotlin** com **Jetpack Compose** e **Material 3**.

---

## 🎯 Objetivo

Fornecer um bloqueador de distrações de alta integridade diretamente no smartphone:
- **Bloqueio de Sites**: Interceptação local de consultas DNS via `VpnService` (sem proxy externo e sem lentidão).
- **Bloqueio de Aplicativos**: Detecção de apps proibidos em primeiro plano via `AccessibilityService`.
- **Tela de Interceptor (Overlay)**: Interface focada com contagem regressiva, mensagem de foco e exercício de respiração (*Box Breathing* 4-4-4-4).
- **Anti-Burla Equilibrado**: Bloqueio irredutível durante sessões ativas (impossível pausar ou desligar no meio do foco), permitindo desinstalação livre quando não houver sessão ativa.

---

## 🏗️ Estrutura do Projeto

```
android/
├── app/
│   ├── src/main/
│   │   ├── AndroidManifest.xml          # Permissões (VPN, Acessibilidade, Overlay)
│   │   ├── java/com/focusguard/app/
│   │   │   ├── MainActivity.kt          # Ponto de entrada e dashboard principal Compose
│   │   │   └── ui/theme/                # Design tokens FocusGuard (Dark Slate & Emerald)
│   │   │       ├── Color.kt
│   │   │       ├── Theme.kt
│   │   │       └── Type.kt
│   │   └── res/                         # Strings, cores, temas e ícones adaptativos
│   ├── build.gradle.kts                 # Configuração do módulo Android (SDK 34 / MinSDK 26)
│   └── proguard-rules.pro
├── gradle/
│   ├── libs.versions.toml               # Version Catalog centralizado
│   └── wrapper/                         # Gradle Wrapper 8.7
├── build.gradle.kts                     # Configuração raiz
├── settings.gradle.kts                  # Declaração do projeto e repositórios
├── gradlew                              # Script de build Linux/macOS
├── gradlew.bat                          # Script de build Windows
└── README.md
```

---

## 🚀 Como Executar

### 1. No Android Studio
1. Abra o Android Studio (versão Hedgehog, Iguana, Ladybug ou superior).
2. Selecione **Open** e escolha a pasta `focus-guard/android`.
3. Aguarde a sincronização inicial do Gradle.
4. Conecte um dispositivo físico Android ou inicie um Emulador (Android 8.0+ / API 26+).
5. Clique em **Run 'app'** (`Shift + F10`).

### 2. Via Linha de Comando
```bash
cd android
./gradlew assembleDebug
```
O APK gerado ficará disponível em:
`app/build/outputs/apk/debug/app-debug.apk`

---

## 📋 Roadmap de Implementação

- [x] **Etapa 1: Estrutura Base** (Gradle Kotlin DSL, Jetpack Compose, Material 3, Tema FocusGuard e MainActivity).
- [ ] **Etapa 2: Serviços do Sistema** (Esqueleto de `FocusVpnService`, `FocusAccessibilityService` e `FocusForegroundService`).
- [ ] **Etapa 3: Telas de Onboarding & Interceptor** (Wizard de permissões guiado e overlay de bloqueio).
- [ ] **Etapa 4: Gerenciamento de Regras & Seletor de Apps** (Listagem de apps instalados e presets).
- [ ] **Etapa 5: Ciclos Pomodoro & Timers** (Temporizadores e anti-tamper de sessão ativa).
