package com.focusguard.app.domain

import android.content.Context
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import android.graphics.drawable.Drawable
import java.util.Locale

data class AppItem(
    val packageName: String,
    val appName: String,
    val isBlocked: Boolean,
    val icon: Drawable? = null
)

object AppSearchFilter {
    fun filter(apps: List<AppItem>, query: String): List<AppItem> {
        val cleanQuery = query.trim().lowercase(Locale.ROOT)
        if (cleanQuery.isEmpty()) {
            return apps
        }
        return apps.filter { app ->
            app.appName.lowercase(Locale.ROOT).contains(cleanQuery) ||
            app.packageName.lowercase(Locale.ROOT).contains(cleanQuery)
        }
    }
}

object AppListManager {

    /**
     * Consulta o PackageManager para listar os aplicativos com interface (lançáveis)
     * instalados no celular do usuário, indicando se estão bloqueados.
     */
    fun getInstalledUserApps(context: Context, presetManager: PresetManager): List<AppItem> {
        val pm = context.packageManager
        val installedApps = pm.getInstalledApplications(PackageManager.GET_META_DATA)
        val result = mutableListOf<AppItem>()

        for (appInfo in installedApps) {
            // Ignora o próprio FocusGuard da lista de bloqueio
            if (appInfo.packageName == context.packageName) {
                continue
            }

            // Exibe apenas apps que o usuário pode abrir (possuem launch intent)
            val launchIntent = pm.getLaunchIntentForPackage(appInfo.packageName)
            if (launchIntent != null) {
                val appName = pm.getApplicationLabel(appInfo).toString()
                val icon = try {
                    pm.getApplicationIcon(appInfo)
                } catch (_: Exception) {
                    null
                }
                val isBlocked = presetManager.isPackageBlocked(appInfo.packageName)

                result.add(
                    AppItem(
                        packageName = appInfo.packageName,
                        appName = appName,
                        isBlocked = isBlocked,
                        icon = icon
                    )
                )
            }
        }

        // Ordena alfabeticamente por nome legível
        return result.sortedBy { it.appName.lowercase(Locale.ROOT) }
    }
}
