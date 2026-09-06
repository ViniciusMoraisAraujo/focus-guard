package com.focusguard.app.ui.apps

import android.graphics.drawable.BitmapDrawable
import android.widget.Toast
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Image
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Block
import androidx.compose.material.icons.filled.Clear
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CheckboxDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextFieldDefaults
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.graphics.drawable.toBitmap
import com.focusguard.app.domain.AppItem
import com.focusguard.app.domain.AppListManager
import com.focusguard.app.domain.AppSearchFilter
import com.focusguard.app.domain.FocusGuardManager
import com.focusguard.app.domain.Preset
import com.focusguard.app.ui.theme.BgDark
import com.focusguard.app.ui.theme.BorderDark
import com.focusguard.app.ui.theme.CyanAccent
import com.focusguard.app.ui.theme.DangerRed
import com.focusguard.app.ui.theme.EmeraldPrimary
import com.focusguard.app.ui.theme.SurfaceDark
import com.focusguard.app.ui.theme.SurfaceVariantDark
import com.focusguard.app.ui.theme.TextMuted
import com.focusguard.app.ui.theme.TextPrimary
import com.focusguard.app.ui.theme.TextSecondary
import androidx.activity.compose.BackHandler
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AppsScreen(
    onBack: () -> Unit
) {
    BackHandler {
        onBack()
    }

    val context = LocalContext.current
    val isFocusActive by FocusGuardManager.isFocusActive.collectAsState()

    var searchQuery by remember { mutableStateOf("") }
    var installedApps by remember { mutableStateOf<List<AppItem>>(emptyList()) }
    var presets by remember { mutableStateOf(FocusGuardManager.presetManager.getPresets()) }
    var refreshTrigger by remember { mutableStateOf(0) }

    // Carrega aplicativos instalados em background para fluidez total da UI
    LaunchedEffect(refreshTrigger) {
        withContext(Dispatchers.IO) {
            val apps = AppListManager.getInstalledUserApps(context, FocusGuardManager.presetManager)
            withContext(Dispatchers.Main) {
                installedApps = apps
                presets = FocusGuardManager.presetManager.getPresets()
            }
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
                            text = "Apps & Sites Bloqueados",
                            style = MaterialTheme.typography.titleMedium,
                            fontWeight = FontWeight.Bold,
                            color = TextPrimary
                        )
                        Text(
                            text = "${FocusGuardManager.presetManager.getAllBlockedPackages().size} itens bloqueados",
                            style = MaterialTheme.typography.labelSmall,
                            color = EmeraldPrimary
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
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding)
                .padding(horizontal = 20.dp)
        ) {
            // Aviso de Anti-burla se a sessão estiver ativa
            if (isFocusActive) {
                Card(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(vertical = 8.dp),
                    colors = CardDefaults.cardColors(containerColor = DangerRed.copy(alpha = 0.12f)),
                    border = BorderStroke(1.dp, DangerRed.copy(alpha = 0.4f)),
                    shape = RoundedCornerShape(12.dp)
                ) {
                    Row(
                        modifier = Modifier.padding(14.dp),
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Icon(
                            imageVector = Icons.Default.Warning,
                            contentDescription = null,
                            tint = DangerRed,
                            modifier = Modifier.size(20.dp)
                        )
                        Spacer(modifier = Modifier.width(10.dp))
                        Text(
                            text = "Sessão de foco em andamento: desativar regras está bloqueado para proteger seu foco.",
                            style = MaterialTheme.typography.bodyMedium,
                            color = TextPrimary,
                            fontSize = 12.sp
                        )
                    }
                }
            }

            // Barra de Busca
            OutlinedTextField(
                value = searchQuery,
                onValueChange = { searchQuery = it },
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(vertical = 10.dp),
                placeholder = { Text("Buscar aplicativo instalado...", color = TextMuted) },
                leadingIcon = {
                    Icon(
                        imageVector = Icons.Default.Search,
                        contentDescription = null,
                        tint = TextSecondary
                    )
                },
                trailingIcon = {
                    if (searchQuery.isNotEmpty()) {
                        IconButton(onClick = { searchQuery = "" }) {
                            Icon(
                                imageVector = Icons.Default.Clear,
                                contentDescription = "Limpar",
                                tint = TextSecondary
                            )
                        }
                    }
                },
                singleLine = true,
                shape = RoundedCornerShape(14.dp),
                colors = TextFieldDefaults.colors(
                    focusedContainerColor = SurfaceDark,
                    unfocusedContainerColor = SurfaceDark,
                    focusedTextColor = TextPrimary,
                    unfocusedTextColor = TextPrimary,
                    cursorColor = EmeraldPrimary,
                    focusedIndicatorColor = EmeraldPrimary,
                    unfocusedIndicatorColor = BorderDark
                )
            )

            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                verticalArrangement = Arrangement.spacedBy(10.dp)
            ) {
                // Seção de Presets Prontos (exibida apenas quando não há busca ativa)
                if (searchQuery.isEmpty()) {
                    item {
                        Text(
                            text = "Categorias Prontas (Presets)",
                            style = MaterialTheme.typography.titleMedium,
                            fontWeight = FontWeight.SemiBold,
                            color = TextPrimary,
                            modifier = Modifier.padding(top = 10.dp, bottom = 4.dp)
                        )
                    }

                    items(presets) { preset ->
                        PresetCard(
                            preset = preset,
                            onToggle = { enabled ->
                                try {
                                    FocusGuardManager.presetManager.setPresetEnabled(preset.id, enabled)
                                    FocusGuardManager.syncPolicies()
                                    refreshTrigger++
                                } catch (e: IllegalStateException) {
                                    Toast.makeText(context, e.message, Toast.LENGTH_LONG).show()
                                }
                            }
                        )
                    }

                    item {
                        Spacer(modifier = Modifier.height(14.dp))
                        Text(
                            text = "Aplicativos Instalados (${installedApps.size})",
                            style = MaterialTheme.typography.titleMedium,
                            fontWeight = FontWeight.SemiBold,
                            color = TextPrimary,
                            modifier = Modifier.padding(bottom = 4.dp)
                        )
                    }
                }

                // Lista de Aplicativos do Usuário
                items(filteredApps, key = { it.packageName }) { appItem ->
                    AppRow(
                        app = appItem,
                        onToggle = { blocked ->
                            try {
                                FocusGuardManager.presetManager.setCustomAppBlocked(appItem.packageName, blocked)
                                FocusGuardManager.syncPolicies()
                                refreshTrigger++
                            } catch (e: IllegalStateException) {
                                Toast.makeText(context, e.message, Toast.LENGTH_LONG).show()
                            }
                        }
                    )
                }

                item {
                    Spacer(modifier = Modifier.height(24.dp))
                }
            }
        }
    }
}

@Composable
fun PresetCard(
    preset: Preset,
    onToggle: (Boolean) -> Unit
) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(containerColor = SurfaceDark),
        shape = RoundedCornerShape(14.dp),
        border = BorderStroke(1.dp, if (preset.isEnabled) EmeraldPrimary.copy(alpha = 0.5f) else BorderDark)
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
                        if (preset.isEnabled) EmeraldPrimary.copy(alpha = 0.15f)
                        else SurfaceVariantDark
                    ),
                contentAlignment = Alignment.Center
            ) {
                Icon(
                    imageVector = Icons.Default.Block,
                    contentDescription = null,
                    tint = if (preset.isEnabled) EmeraldPrimary else TextSecondary,
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
                    text = preset.description,
                    style = MaterialTheme.typography.bodyMedium,
                    color = TextSecondary,
                    fontSize = 12.sp
                )
            }

            Switch(
                checked = preset.isEnabled,
                onCheckedChange = onToggle,
                colors = SwitchDefaults.colors(
                    checkedThumbColor = BgDark,
                    checkedTrackColor = EmeraldPrimary,
                    uncheckedThumbColor = TextSecondary,
                    uncheckedTrackColor = SurfaceVariantDark
                )
            )
        }
    }
}

@Composable
fun AppRow(
    app: AppItem,
    onToggle: (Boolean) -> Unit
) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(containerColor = SurfaceDark),
        shape = RoundedCornerShape(12.dp),
        border = BorderStroke(1.dp, BorderDark)
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(12.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            // Ícone real do app instalado
            if (app.icon != null) {
                val bitmap = remember(app.icon) {
                    try {
                        app.icon.toBitmap(width = 96, height = 96).asImageBitmap()
                    } catch (_: Exception) {
                        null
                    }
                }
                if (bitmap != null) {
                    Image(
                        bitmap = bitmap,
                        contentDescription = app.appName,
                        modifier = Modifier
                            .size(40.dp)
                            .clip(RoundedCornerShape(10.dp))
                    )
                } else {
                    DefaultAppIcon()
                }
            } else {
                DefaultAppIcon()
            }

            Spacer(modifier = Modifier.width(14.dp))

            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = app.appName,
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.Medium,
                    color = TextPrimary,
                    fontSize = 15.sp
                )
                Text(
                    text = app.packageName,
                    style = MaterialTheme.typography.labelSmall,
                    color = TextMuted,
                    fontSize = 11.sp
                )
            }

            Checkbox(
                checked = app.isBlocked,
                onCheckedChange = onToggle,
                colors = CheckboxDefaults.colors(
                    checkedColor = EmeraldPrimary,
                    checkmarkColor = BgDark,
                    uncheckedColor = TextSecondary
                )
            )
        }
    }
}

@Composable
fun DefaultAppIcon() {
    Box(
        modifier = Modifier
            .size(40.dp)
            .clip(RoundedCornerShape(10.dp))
            .background(SurfaceVariantDark),
        contentAlignment = Alignment.Center
    ) {
        Icon(
            imageVector = Icons.Default.Block,
            contentDescription = null,
            tint = TextSecondary,
            modifier = Modifier.size(20.dp)
        )
    }
}
