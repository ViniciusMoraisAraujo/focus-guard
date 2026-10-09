package com.focusguard.app.ui.block

import android.content.Context
import android.widget.Toast
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Apps
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.HourglassEmpty
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Shield
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.FilterChipDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Tab
import androidx.compose.material3.TabRow
import androidx.compose.material3.TabRowDefaults
import androidx.compose.material3.TabRowDefaults.tabIndicatorOffset
import androidx.compose.material3.Text
import androidx.compose.material3.TextFieldDefaults
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.graphics.drawable.toBitmap
import com.focusguard.app.domain.AppItem
import com.focusguard.app.domain.AppListManager
import com.focusguard.app.domain.AppSearchFilter
import com.focusguard.app.domain.BlockTargetType
import com.focusguard.app.domain.FocusGuardManager
import com.focusguard.app.domain.TimedBlock
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
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.withContext
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

private data class DurationOption(
    val label: String,
    val minutes: Int
)

private val PREDEFINED_DURATIONS = listOf(
    DurationOption("30 min", 30),
    DurationOption("1 h", 60),
    DurationOption("2 h", 120),
    DurationOption("4 h", 240),
    DurationOption("8 h", 480)
)

@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun BlockScreen(
    onBack: () -> Unit
) {
    BackHandler {
        onBack()
    }

    val context = LocalContext.current
    var selectedTab by remember { mutableIntStateOf(0) } // 0 = Categoria, 1 = Aplicativo

    // Categoria selecionada
    val presets = remember { FocusGuardManager.presetManager.getPresets() }
    var selectedPresetId by remember { mutableStateOf(presets.firstOrNull()?.id ?: "social") }

    // App selecionado
    var installedApps by remember { mutableStateOf<List<AppItem>>(emptyList()) }
    var searchQuery by remember { mutableStateOf("") }
    var selectedApp by remember { mutableStateOf<AppItem?>(null) }

    // Duração selecionada
    var selectedDurationMinutes by remember { mutableIntStateOf(60) }
    var isCustomDuration by remember { mutableStateOf(false) }
    var customMinutesText by remember { mutableStateOf("") }

    // Carrega apps em segundo plano
    LaunchedEffect(Unit) {
        withContext(Dispatchers.IO) {
            val apps = AppListManager.getInstalledUserApps(context, FocusGuardManager.presetManager)
            withContext(Dispatchers.Main) {
                installedApps = apps
                if (selectedApp == null && apps.isNotEmpty()) {
                    selectedApp = apps.first()
                }
            }
        }
    }

    // Bloqueios ativos atualizados
    val activeBlocks by FocusGuardManager.timedBlockManager.blocksFlow.collectAsState()
    var tick by remember { mutableLongStateOf(0L) }
    LaunchedEffect(Unit) {
        while (isActive) {
            delay(1000L)
            tick++
        }
    }

    val filteredApps = remember(installedApps, searchQuery) {
        AppSearchFilter.filter(installedApps, searchQuery)
    }

    Scaffold(
        containerColor = BgDark,
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(
                            text = "Bloquear",
                            style = MaterialTheme.typography.titleMedium,
                            fontWeight = FontWeight.Bold,
                            color = TextPrimary
                        )
                        Text(
                            text = "Escolha um alvo e a duração do bloqueio",
                            style = MaterialTheme.typography.labelSmall,
                            color = TextSecondary
                        )
                    }
                },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(
                            imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                            contentDescription = "Voltar",
                            tint = TextPrimary
                        )
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(containerColor = BgDark)
            )
        }
    ) { innerPadding ->
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding)
                .padding(horizontal = 20.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp)
        ) {
            // 1. Seleção de Modo (Categoria vs Aplicativo)
            item {
                TabRow(
                    selectedTabIndex = selectedTab,
                    containerColor = SurfaceDark,
                    contentColor = EmeraldPrimary,
                    indicator = { tabPositions ->
                        TabRowDefaults.SecondaryIndicator(
                            modifier = Modifier.tabIndicatorOffset(tabPositions[selectedTab]),
                            color = EmeraldPrimary
                        )
                    },
                    modifier = Modifier.clip(RoundedCornerShape(12.dp))
                ) {
                    Tab(
                        selected = selectedTab == 0,
                        onClick = { selectedTab = 0 },
                        text = {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Icon(Icons.Default.Shield, contentDescription = null, modifier = Modifier.size(18.dp))
                                Spacer(modifier = Modifier.width(8.dp))
                                Text("Categoria", fontWeight = FontWeight.SemiBold)
                            }
                        }
                    )
                    Tab(
                        selected = selectedTab == 1,
                        onClick = { selectedTab = 1 },
                        text = {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Icon(Icons.Default.Apps, contentDescription = null, modifier = Modifier.size(18.dp))
                                Spacer(modifier = Modifier.width(8.dp))
                                Text("Aplicativo", fontWeight = FontWeight.SemiBold)
                            }
                        }
                    )
                }
            }

            // 2. Painel de Seleção de Alvo
            if (selectedTab == 0) {
                // Modo Categoria
                item {
                    Text(
                        text = "Escolha uma categoria para bloquear:",
                        style = MaterialTheme.typography.titleSmall,
                        color = TextSecondary,
                        fontWeight = FontWeight.Medium
                    )
                }
                items(presets) { preset ->
                    val isSelected = preset.id == selectedPresetId
                    Card(
                        onClick = { selectedPresetId = preset.id },
                        colors = CardDefaults.cardColors(
                            containerColor = if (isSelected) SurfaceVariantDark else SurfaceDark
                        ),
                        shape = RoundedCornerShape(14.dp),
                        border = BorderStroke(
                            1.5.dp,
                            if (isSelected) EmeraldPrimary else BorderDark
                        ),
                        modifier = Modifier.fillMaxWidth()
                    ) {
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(16.dp),
                            verticalAlignment = Alignment.CenterVertically
                        ) {
                            Box(
                                modifier = Modifier
                                    .size(40.dp)
                                    .clip(RoundedCornerShape(10.dp))
                                    .background(
                                        if (isSelected) EmeraldPrimary.copy(alpha = 0.2f)
                                        else SurfaceDark
                                    ),
                                contentAlignment = Alignment.Center
                            ) {
                                Icon(
                                    imageVector = if (isSelected) Icons.Default.CheckCircle else Icons.Default.Shield,
                                    contentDescription = null,
                                    tint = if (isSelected) EmeraldPrimary else TextSecondary,
                                    modifier = Modifier.size(22.dp)
                                )
                            }
                            Spacer(modifier = Modifier.width(14.dp))
                            Column(modifier = Modifier.weight(1f)) {
                                Text(
                                    text = preset.name,
                                    style = MaterialTheme.typography.titleMedium,
                                    fontWeight = FontWeight.SemiBold,
                                    color = TextPrimary
                                )
                                Text(
                                    text = "${preset.apps.size} aplicativos incluídos",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = TextSecondary
                                )
                            }
                        }
                    }
                }
            } else {
                // Modo Aplicativo
                item {
                    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        Text(
                            text = "Selecione o aplicativo a bloquear:",
                            style = MaterialTheme.typography.titleSmall,
                            color = TextSecondary,
                            fontWeight = FontWeight.Medium
                        )

                        OutlinedTextField(
                            value = searchQuery,
                            onValueChange = { searchQuery = it },
                            placeholder = { Text("Buscar aplicativo...", color = TextMuted) },
                            leadingIcon = {
                                Icon(Icons.Default.Search, contentDescription = null, tint = TextMuted)
                            },
                            singleLine = true,
                            colors = TextFieldDefaults.colors(
                                focusedContainerColor = SurfaceDark,
                                unfocusedContainerColor = SurfaceDark,
                                focusedTextColor = TextPrimary,
                                unfocusedTextColor = TextPrimary,
                                focusedIndicatorColor = EmeraldPrimary,
                                unfocusedIndicatorColor = BorderDark
                            ),
                            shape = RoundedCornerShape(12.dp),
                            modifier = Modifier.fillMaxWidth()
                        )
                    }
                }

                item {
                    Card(
                        colors = CardDefaults.cardColors(containerColor = SurfaceDark),
                        shape = RoundedCornerShape(14.dp),
                        border = BorderStroke(1.dp, BorderDark),
                        modifier = Modifier
                            .fillMaxWidth()
                            .height(200.dp)
                    ) {
                        LazyColumn(
                            modifier = Modifier
                                .fillMaxSize()
                                .padding(8.dp)
                        ) {
                            items(filteredApps) { app ->
                                val isSelected = selectedApp?.packageName == app.packageName
                                Surface(
                                    onClick = { selectedApp = app },
                                    shape = RoundedCornerShape(8.dp),
                                    color = if (isSelected) EmeraldPrimary.copy(alpha = 0.15f) else SurfaceDark,
                                    border = BorderStroke(
                                        1.dp,
                                        if (isSelected) EmeraldPrimary else BorderDark.copy(alpha = 0.5f)
                                    ),
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .padding(vertical = 3.dp)
                                ) {
                                    Row(
                                        modifier = Modifier
                                            .fillMaxWidth()
                                            .padding(horizontal = 10.dp, vertical = 8.dp),
                                        verticalAlignment = Alignment.CenterVertically
                                    ) {
                                        app.icon?.toBitmap(width = 96, height = 96)?.asImageBitmap()?.let { bmp ->
                                            Image(
                                                bitmap = bmp,
                                                contentDescription = app.appName,
                                                modifier = Modifier
                                                    .size(32.dp)
                                                    .clip(RoundedCornerShape(6.dp))
                                            )
                                        } ?: Box(
                                            modifier = Modifier
                                                .size(32.dp)
                                                .clip(RoundedCornerShape(6.dp))
                                                .background(SurfaceVariantDark),
                                            contentAlignment = Alignment.Center
                                        ) {
                                            Icon(Icons.Default.Apps, contentDescription = null, tint = TextMuted, modifier = Modifier.size(18.dp))
                                        }

                                        Spacer(modifier = Modifier.width(10.dp))
                                        Text(
                                            text = app.appName,
                                            style = MaterialTheme.typography.bodyMedium,
                                            fontWeight = if (isSelected) FontWeight.Bold else FontWeight.Normal,
                                            color = if (isSelected) EmeraldPrimary else TextPrimary,
                                            modifier = Modifier.weight(1f)
                                        )

                                        if (isSelected) {
                                            Icon(
                                                Icons.Default.CheckCircle,
                                                contentDescription = "Selecionado",
                                                tint = EmeraldPrimary,
                                                modifier = Modifier.size(18.dp)
                                            )
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }

            // 3. Seleção de Duração
            item {
                Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Text(
                        text = "Duração do bloqueio:",
                        style = MaterialTheme.typography.titleSmall,
                        color = TextSecondary,
                        fontWeight = FontWeight.Medium
                    )

                    FlowRow(
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                        modifier = Modifier.fillMaxWidth()
                    ) {
                        PREDEFINED_DURATIONS.forEach { option ->
                            val isSelected = !isCustomDuration && selectedDurationMinutes == option.minutes
                            FilterChip(
                                selected = isSelected,
                                onClick = {
                                    isCustomDuration = false
                                    selectedDurationMinutes = option.minutes
                                },
                                label = { Text(option.label, fontWeight = FontWeight.SemiBold) },
                                colors = FilterChipDefaults.filterChipColors(
                                    selectedContainerColor = EmeraldPrimary.copy(alpha = 0.2f),
                                    selectedLabelColor = EmeraldPrimary,
                                    containerColor = SurfaceDark,
                                    labelColor = TextSecondary
                                ),
                                border = BorderStroke(
                                    1.dp,
                                    if (isSelected) EmeraldPrimary else BorderDark
                                )
                            )
                        }

                        FilterChip(
                            selected = isCustomDuration,
                            onClick = { isCustomDuration = true },
                            label = { Text("Personalizado", fontWeight = FontWeight.SemiBold) },
                            colors = FilterChipDefaults.filterChipColors(
                                selectedContainerColor = EmeraldPrimary.copy(alpha = 0.2f),
                                selectedLabelColor = EmeraldPrimary,
                                containerColor = SurfaceDark,
                                labelColor = TextSecondary
                            ),
                            border = BorderStroke(
                                1.dp,
                                if (isCustomDuration) EmeraldPrimary else BorderDark
                            )
                        )
                    }

                    if (isCustomDuration) {
                        OutlinedTextField(
                            value = customMinutesText,
                            onValueChange = { customMinutesText = it.filter { ch -> ch.isDigit() } },
                            label = { Text("Duração em minutos") },
                            placeholder = { Text("ex: 45", color = TextMuted) },
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                            singleLine = true,
                            colors = TextFieldDefaults.colors(
                                focusedContainerColor = SurfaceDark,
                                unfocusedContainerColor = SurfaceDark,
                                focusedTextColor = TextPrimary,
                                unfocusedTextColor = TextPrimary,
                                focusedIndicatorColor = EmeraldPrimary,
                                unfocusedIndicatorColor = BorderDark
                            ),
                            shape = RoundedCornerShape(12.dp),
                            modifier = Modifier.fillMaxWidth()
                        )
                    }
                }
            }

            // 4. Botão de Ação: Bloquear
            item {
                val targetName = if (selectedTab == 0) {
                    presets.find { it.id == selectedPresetId }?.name ?: "Categoria"
                } else {
                    selectedApp?.appName ?: "Aplicativo"
                }

                Button(
                    onClick = {
                        val durationMinutes = if (isCustomDuration) {
                            val parsed = customMinutesText.toIntOrNull()
                            if (parsed == null || parsed <= 0) {
                                Toast.makeText(context, "Digite uma duração válida em minutos!", Toast.LENGTH_SHORT).show()
                                return@Button
                            }
                            parsed
                        } else {
                            selectedDurationMinutes
                        }

                        val durationMs = durationMinutes * 60 * 1000L

                        if (selectedTab == 0) {
                            val preset = presets.find { it.id == selectedPresetId }
                            if (preset != null) {
                                FocusGuardManager.blockPreset(preset.id, preset.name, durationMs)
                                FocusForegroundService.sync(context)
                                Toast.makeText(context, "Categoria '${preset.name}' bloqueada por ${durationMinutes}m!", Toast.LENGTH_LONG).show()
                            }
                        } else {
                            val app = selectedApp
                            if (app != null) {
                                FocusGuardManager.blockApp(app.packageName, app.appName, durationMs)
                                FocusForegroundService.sync(context)
                                Toast.makeText(context, "'${app.appName}' bloqueado por ${durationMinutes}m!", Toast.LENGTH_LONG).show()
                            } else {
                                Toast.makeText(context, "Selecione um aplicativo para bloquear!", Toast.LENGTH_SHORT).show()
                            }
                        }
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
                    Icon(Icons.Default.Lock, contentDescription = null, modifier = Modifier.size(20.dp))
                    Spacer(modifier = Modifier.width(8.dp))
                    Text(
                        text = "Bloquear $targetName",
                        fontSize = 16.sp,
                        fontWeight = FontWeight.Bold
                    )
                }
            }

            // 5. Seção de Bloqueios Ativos
            item {
                Spacer(modifier = Modifier.height(8.dp))
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Text(
                        text = "Bloqueios Ativos",
                        style = MaterialTheme.typography.titleMedium,
                        fontWeight = FontWeight.Bold,
                        color = TextPrimary
                    )
                    Spacer(modifier = Modifier.width(8.dp))
                    Box(
                        modifier = Modifier
                            .clip(CircleShape)
                            .background(if (activeBlocks.isNotEmpty()) EmeraldPrimary else SurfaceVariantDark)
                            .padding(horizontal = 8.dp, vertical = 2.dp)
                    ) {
                        Text(
                            text = "${activeBlocks.size}",
                            style = MaterialTheme.typography.labelSmall,
                            fontWeight = FontWeight.Bold,
                            color = if (activeBlocks.isNotEmpty()) BgDark else TextMuted
                        )
                    }
                }
            }

            if (activeBlocks.isEmpty()) {
                item {
                    Card(
                        colors = CardDefaults.cardColors(containerColor = SurfaceDark),
                        shape = RoundedCornerShape(14.dp),
                        border = BorderStroke(1.dp, BorderDark),
                        modifier = Modifier.fillMaxWidth()
                    ) {
                        Column(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(24.dp),
                            horizontalAlignment = Alignment.CenterHorizontally
                        ) {
                            Icon(
                                imageVector = Icons.Default.HourglassEmpty,
                                contentDescription = null,
                                tint = TextMuted,
                                modifier = Modifier.size(32.dp)
                            )
                            Spacer(modifier = Modifier.height(8.dp))
                            Text(
                                text = "Nenhum bloqueio temporizado ativo",
                                style = MaterialTheme.typography.bodyMedium,
                                color = TextMuted
                            )
                        }
                    }
                }
            } else {
                items(activeBlocks) { block ->
                    ActiveBlockCard(
                        block = block,
                        context = context,
                        tick = tick
                    )
                }
            }

            item {
                Spacer(modifier = Modifier.height(24.dp))
            }
        }
    }
}

@Composable
private fun ActiveBlockCard(
    block: TimedBlock,
    context: Context,
    tick: Long
) {
    val remainingMs = block.remainingTimeMs(android.os.SystemClock.elapsedRealtime())
    val remainingFormatted = formatRemaining(remainingMs)
    val expiresTimeFormatted = formatWallTime(block.wallExpiresAtMs)

    Card(
        colors = CardDefaults.cardColors(containerColor = SurfaceDark),
        shape = RoundedCornerShape(14.dp),
        border = BorderStroke(1.dp, BorderDark),
        modifier = Modifier.fillMaxWidth()
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(16.dp)
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically
            ) {
                Box(
                    modifier = Modifier
                        .size(36.dp)
                        .clip(RoundedCornerShape(8.dp))
                        .background(EmeraldPrimary.copy(alpha = 0.15f)),
                    contentAlignment = Alignment.Center
                ) {
                    Icon(
                        imageVector = if (block.targetType == BlockTargetType.PRESET) Icons.Default.Shield else Icons.Default.Apps,
                        contentDescription = null,
                        tint = EmeraldPrimary,
                        modifier = Modifier.size(20.dp)
                    )
                }
                Spacer(modifier = Modifier.width(12.dp))
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = block.displayName,
                        style = MaterialTheme.typography.titleMedium,
                        fontWeight = FontWeight.Bold,
                        color = TextPrimary
                    )
                    Text(
                        text = if (block.targetType == BlockTargetType.PRESET) "Categoria" else "Aplicativo",
                        style = MaterialTheme.typography.labelSmall,
                        color = TextMuted
                    )
                }
                Surface(
                    color = CyanAccent.copy(alpha = 0.15f),
                    shape = RoundedCornerShape(8.dp),
                    border = BorderStroke(1.dp, CyanAccent.copy(alpha = 0.4f))
                ) {
                    Text(
                        text = remainingFormatted,
                        style = MaterialTheme.typography.labelMedium,
                        fontWeight = FontWeight.Bold,
                        color = CyanAccent,
                        modifier = Modifier.padding(horizontal = 10.dp, vertical = 4.dp)
                    )
                }
            }

            Spacer(modifier = Modifier.height(12.dp))

            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text(
                    text = "🔒 Expira às $expiresTimeFormatted",
                    style = MaterialTheme.typography.bodySmall,
                    color = TextSecondary
                )

                // Botões para estender o bloqueio (anti-burla permite aumentar)
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    OutlinedButton(
                        onClick = {
                            FocusGuardManager.timedBlockManager.extendBlock(block.id, 30 * 60 * 1000L)
                            FocusForegroundService.sync(context)
                            Toast.makeText(context, "+30 min adicionados a ${block.displayName}", Toast.LENGTH_SHORT).show()
                        },
                        shape = RoundedCornerShape(8.dp),
                        border = BorderStroke(1.dp, BorderDark),
                        colors = ButtonDefaults.outlinedButtonColors(contentColor = EmeraldPrimary),
                        modifier = Modifier.height(32.dp)
                    ) {
                        Icon(Icons.Default.Add, contentDescription = null, modifier = Modifier.size(14.dp))
                        Spacer(modifier = Modifier.width(4.dp))
                        Text("+30m", fontSize = 12.sp)
                    }

                    OutlinedButton(
                        onClick = {
                            FocusGuardManager.timedBlockManager.extendBlock(block.id, 60 * 60 * 1000L)
                            FocusForegroundService.sync(context)
                            Toast.makeText(context, "+1h adicionada a ${block.displayName}", Toast.LENGTH_SHORT).show()
                        },
                        shape = RoundedCornerShape(8.dp),
                        border = BorderStroke(1.dp, BorderDark),
                        colors = ButtonDefaults.outlinedButtonColors(contentColor = EmeraldPrimary),
                        modifier = Modifier.height(32.dp)
                    ) {
                        Icon(Icons.Default.Add, contentDescription = null, modifier = Modifier.size(14.dp))
                        Spacer(modifier = Modifier.width(4.dp))
                        Text("+1h", fontSize = 12.sp)
                    }
                }
            }
        }
    }
}

private fun formatRemaining(ms: Long): String {
    val totalSec = ms / 1000L
    val hours = totalSec / 3600L
    val min = (totalSec % 3600L) / 60L
    val sec = totalSec % 60L
    return if (hours > 0) {
        String.format(Locale.getDefault(), "%dh %02dm", hours, min)
    } else {
        String.format(Locale.getDefault(), "%02d:%02d", min, sec)
    }
}

private fun formatWallTime(ms: Long): String {
    val sdf = SimpleDateFormat("HH:mm", Locale.getDefault())
    return sdf.format(Date(ms))
}
