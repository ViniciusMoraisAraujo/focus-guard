package com.focusguard.app.ui.pomodoro

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Flag
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material.icons.filled.Timer
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.focusguard.app.domain.FocusGuardManager
import com.focusguard.app.domain.PomodoroPhase
import com.focusguard.app.domain.PomodoroState
import com.focusguard.app.service.FocusForegroundService
import com.focusguard.app.ui.theme.BgDark
import com.focusguard.app.ui.theme.BorderDark
import com.focusguard.app.ui.theme.CyanAccent
import com.focusguard.app.ui.theme.EmeraldPrimary
import com.focusguard.app.ui.theme.SurfaceDark
import com.focusguard.app.ui.theme.SurfaceVariantDark
import com.focusguard.app.ui.theme.TextMuted
import com.focusguard.app.ui.theme.TextPrimary
import com.focusguard.app.ui.theme.TextSecondary
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive

@Composable
fun PomodoroScreen(
    onBack: () -> Unit
) {
    val context = LocalContext.current
    var pomodoroState by remember {
        mutableStateOf(FocusGuardManager.pomodoroEngine.getState())
    }
    var missionInput by remember { mutableStateOf("") }

    // Loop de sincronização periódica do temporizador do motor Pomodoro
    LaunchedEffect(Unit) {
        while (isActive) {
            pomodoroState = FocusGuardManager.pomodoroEngine.getState()
            FocusGuardManager.checkStatus()
            delay(500L)
        }
    }

    val phaseColor = when (pomodoroState.phase) {
        PomodoroPhase.WORK -> EmeraldPrimary
        PomodoroPhase.REST, PomodoroPhase.LONG_REST -> CyanAccent
        PomodoroPhase.IDLE -> TextSecondary
    }

    val phaseLabel = when (pomodoroState.phase) {
        PomodoroPhase.WORK -> "FOCO ATIVO"
        PomodoroPhase.REST -> "DESCANSO CURTO"
        PomodoroPhase.LONG_REST -> "DESCANSO LONGO"
        PomodoroPhase.IDLE -> "PRONTO"
    }

    val remainingMinutes = (pomodoroState.remainingTimeMs / 1000L) / 60L
    val remainingSeconds = (pomodoroState.remainingTimeMs / 1000L) % 60L
    val timeFormatted = if (pomodoroState.phase == PomodoroPhase.IDLE) {
        "25:00"
    } else {
        String.format("%02d:%02d", remainingMinutes, remainingSeconds)
    }

    Scaffold(
        containerColor = BgDark
    ) { innerPadding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding)
                .padding(horizontal = 20.dp, vertical = 16.dp),
            horizontalAlignment = Alignment.CenterHorizontally
        ) {
            // Top Bar
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically
            ) {
                IconButton(onClick = onBack) {
                    Icon(
                        imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                        contentDescription = "Voltar",
                        tint = TextPrimary
                    )
                }
                Spacer(modifier = Modifier.width(8.dp))
                Column {
                    Text(
                        text = "Modo Pomodoro",
                        style = MaterialTheme.typography.titleLarge,
                        fontWeight = FontWeight.Bold,
                        color = TextPrimary
                    )
                    Text(
                        text = "Ciclos de Foco & Produtividade",
                        style = MaterialTheme.typography.bodySmall,
                        color = TextSecondary
                    )
                }
            }

            Spacer(modifier = Modifier.height(24.dp))

            // Ciclo Indicator (4 bolinhas indicando progresso de ciclos)
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.Center,
                verticalAlignment = Alignment.CenterVertically
            ) {
                val activeCycle = (pomodoroState.completedCycles % 4) + 1
                Text(
                    text = "Ciclo $activeCycle de 4",
                    style = MaterialTheme.typography.bodyMedium,
                    color = TextSecondary,
                    fontWeight = FontWeight.Medium
                )
                Spacer(modifier = Modifier.width(12.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    for (i in 1..4) {
                        val isFilled = i <= (pomodoroState.completedCycles % 4)
                        val isCurrent = i == activeCycle && pomodoroState.phase != PomodoroPhase.IDLE
                        Box(
                            modifier = Modifier
                                .size(if (isCurrent) 10.dp else 8.dp)
                                .clip(CircleShape)
                                .background(
                                    when {
                                        isFilled -> EmeraldPrimary
                                        isCurrent -> CyanAccent
                                        else -> SurfaceVariantDark
                                    }
                                )
                        )
                    }
                }
            }

            Spacer(modifier = Modifier.height(32.dp))

            // Circular Timer
            Box(
                contentAlignment = Alignment.Center,
                modifier = Modifier.size(240.dp)
            ) {
                // Background Track
                CircularProgressIndicator(
                    progress = { 1f },
                    modifier = Modifier.size(230.dp),
                    color = SurfaceVariantDark,
                    strokeWidth = 10.dp,
                    trackColor = SurfaceVariantDark
                )

                // Active Progress Track
                val progressValue = if (pomodoroState.phase == PomodoroPhase.IDLE) 0f else pomodoroState.progress
                CircularProgressIndicator(
                    progress = { progressValue },
                    modifier = Modifier.size(230.dp),
                    color = phaseColor,
                    strokeWidth = 10.dp,
                    trackColor = SurfaceVariantDark
                )

                // Central Counter Text
                Column(
                    horizontalAlignment = Alignment.CenterHorizontally
                ) {
                    Text(
                        text = phaseLabel,
                        style = MaterialTheme.typography.labelMedium,
                        fontWeight = FontWeight.Bold,
                        color = phaseColor,
                        letterSpacing = 1.5.sp
                    )
                    Spacer(modifier = Modifier.height(6.dp))
                    Text(
                        text = timeFormatted,
                        style = MaterialTheme.typography.headlineLarge,
                        fontSize = 46.sp,
                        fontWeight = FontWeight.Bold,
                        color = TextPrimary
                    )
                    if (pomodoroState.mission.isNotBlank()) {
                        Spacer(modifier = Modifier.height(6.dp))
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            modifier = Modifier.padding(horizontal = 16.dp)
                        ) {
                            Icon(
                                imageVector = Icons.Default.Flag,
                                contentDescription = null,
                                tint = EmeraldPrimary,
                                modifier = Modifier.size(14.dp)
                            )
                            Spacer(modifier = Modifier.width(4.dp))
                            Text(
                                text = pomodoroState.mission,
                                style = MaterialTheme.typography.bodySmall,
                                color = TextSecondary,
                                maxLines = 1
                            )
                        }
                    }
                }
            }

            Spacer(modifier = Modifier.height(32.dp))

            // Mission input (quando IDLE)
            if (pomodoroState.phase == PomodoroPhase.IDLE) {
                OutlinedTextField(
                    value = missionInput,
                    onValueChange = { missionInput = it },
                    label = { Text("Qual é a sua missão para este bloco?") },
                    placeholder = { Text("ex: Finalizar relatório, Estudar algoritmos") },
                    modifier = Modifier.fillMaxWidth(),
                    shape = RoundedCornerShape(12.dp),
                    colors = OutlinedTextFieldDefaults.colors(
                        focusedContainerColor = SurfaceDark,
                        unfocusedContainerColor = SurfaceDark,
                        focusedBorderColor = EmeraldPrimary,
                        unfocusedBorderColor = BorderDark,
                        focusedLabelColor = EmeraldPrimary,
                        unfocusedLabelColor = TextSecondary,
                        focusedTextColor = TextPrimary,
                        unfocusedTextColor = TextPrimary
                    ),
                    singleLine = true
                )
            } else {
                Card(
                    modifier = Modifier.fillMaxWidth(),
                    shape = RoundedCornerShape(14.dp),
                    colors = CardDefaults.cardColors(containerColor = SurfaceDark),
                    border = BorderStroke(1.dp, BorderDark)
                ) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(16.dp),
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Box(
                            modifier = Modifier
                                .size(36.dp)
                                .clip(CircleShape)
                                .background(phaseColor.copy(alpha = 0.15f)),
                            contentAlignment = Alignment.Center
                        ) {
                            Icon(
                                imageVector = if (pomodoroState.phase == PomodoroPhase.WORK) Icons.Default.Lock else Icons.Default.Timer,
                                contentDescription = null,
                                tint = phaseColor,
                                modifier = Modifier.size(18.dp)
                            )
                        }
                        Spacer(modifier = Modifier.width(12.dp))
                        Column {
                            Text(
                                text = if (pomodoroState.phase == PomodoroPhase.WORK) "Bloqueio Rígido Ativo" else "Intervalo para Recarregar",
                                style = MaterialTheme.typography.titleSmall,
                                fontWeight = FontWeight.SemiBold,
                                color = TextPrimary
                            )
                            Text(
                                text = if (pomodoroState.phase == PomodoroPhase.WORK)
                                    "Distrações bloqueadas até o término do ciclo."
                                else
                                    "Aproveite para levantar, beber água e respirar.",
                                style = MaterialTheme.typography.bodySmall,
                                color = TextMuted
                            )
                        }
                    }
                }
            }

            Spacer(modifier = Modifier.weight(1f))

            // Action Buttons
            when (pomodoroState.phase) {
                PomodoroPhase.IDLE -> {
                    Button(
                        onClick = {
                            FocusGuardManager.startPomodoro(missionInput)
                            FocusForegroundService.start(
                                context,
                                FocusGuardManager.pomodoroEngine.workDurationMs
                            )
                            pomodoroState = FocusGuardManager.pomodoroEngine.getState()
                        },
                        modifier = Modifier
                            .fillMaxWidth()
                            .height(54.dp),
                        shape = RoundedCornerShape(14.dp),
                        colors = ButtonDefaults.buttonColors(
                            containerColor = EmeraldPrimary,
                            contentColor = BgDark
                        )
                    ) {
                        Icon(
                            imageVector = Icons.Default.PlayArrow,
                            contentDescription = null,
                            modifier = Modifier.size(20.dp)
                        )
                        Spacer(modifier = Modifier.width(8.dp))
                        Text(
                            text = "Iniciar Pomodoro (25 min)",
                            fontWeight = FontWeight.Bold,
                            fontSize = 16.sp
                        )
                    }
                }
                PomodoroPhase.WORK -> {
                    // Anti-burla: Botão desativado durante WORK
                    Button(
                        onClick = { /* Bloqueado durante foco ativo */ },
                        enabled = false,
                        modifier = Modifier
                            .fillMaxWidth()
                            .height(54.dp),
                        shape = RoundedCornerShape(14.dp),
                        colors = ButtonDefaults.buttonColors(
                            disabledContainerColor = SurfaceVariantDark,
                            disabledContentColor = TextMuted
                        )
                    ) {
                        Icon(
                            imageVector = Icons.Default.Lock,
                            contentDescription = null,
                            modifier = Modifier.size(18.dp)
                        )
                        Spacer(modifier = Modifier.width(8.dp))
                        Text(
                            text = "Foco em Andamento (Anti-burla)",
                            fontWeight = FontWeight.Medium
                        )
                    }
                }
                PomodoroPhase.REST, PomodoroPhase.LONG_REST -> {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.spacedBy(10.dp)
                    ) {
                        OutlinedButton(
                            onClick = {
                                if (FocusGuardManager.pomodoroEngine.canCancel()) {
                                    FocusGuardManager.pomodoroEngine.stop()
                                    FocusForegroundService.stop(context)
                                    pomodoroState = FocusGuardManager.pomodoroEngine.getState()
                                }
                            },
                            modifier = Modifier
                                .weight(1f)
                                .height(52.dp),
                            shape = RoundedCornerShape(12.dp),
                            border = BorderStroke(1.dp, BorderDark),
                            colors = ButtonDefaults.outlinedButtonColors(contentColor = TextSecondary)
                        ) {
                            Icon(
                                imageVector = Icons.Default.Stop,
                                contentDescription = null,
                                modifier = Modifier.size(18.dp)
                            )
                            Spacer(modifier = Modifier.width(6.dp))
                            Text("Encerrar")
                        }

                        Button(
                            onClick = {
                                FocusGuardManager.pomodoroEngine.skipBreak()
                                FocusForegroundService.start(
                                    context,
                                    FocusGuardManager.pomodoroEngine.workDurationMs
                                )
                                pomodoroState = FocusGuardManager.pomodoroEngine.getState()
                            },
                            modifier = Modifier
                                .weight(1.3f)
                                .height(52.dp),
                            shape = RoundedCornerShape(12.dp),
                            colors = ButtonDefaults.buttonColors(
                                containerColor = EmeraldPrimary,
                                contentColor = BgDark
                            )
                        ) {
                            Icon(
                                imageVector = Icons.Default.SkipNext,
                                contentDescription = null,
                                modifier = Modifier.size(20.dp)
                            )
                            Spacer(modifier = Modifier.width(6.dp))
                            Text("Pular & Focar", fontWeight = FontWeight.Bold)
                        }
                    }
                }
            }
        }
    }
}
