package com.focusguard.app.ui.interceptor

import android.content.Context
import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.OnBackPressedCallback
import androidx.activity.compose.setContent
import com.focusguard.app.ui.theme.FocusGuardTheme

class InterceptorActivity : ComponentActivity() {

    companion object {
        const val EXTRA_BLOCKED_PACKAGE = "extra_blocked_package"

        fun launch(context: Context, blockedPackage: String) {
            val intent = Intent(context, InterceptorActivity::class.java).apply {
                flags = Intent.FLAG_ACTIVITY_NEW_TASK or
                        Intent.FLAG_ACTIVITY_CLEAR_TOP or
                        Intent.FLAG_ACTIVITY_NO_ANIMATION
                putExtra(EXTRA_BLOCKED_PACKAGE, blockedPackage)
            }
            context.startActivity(intent)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // Previne voltar para o aplicativo proibido ao pressionar o botão 'Voltar' do celular
        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                returnToHomeScreen()
            }
        })

        val blockedPackage = intent?.getStringExtra(EXTRA_BLOCKED_PACKAGE) ?: ""

        setContent {
            FocusGuardTheme {
                InterceptorScreen(
                    blockedPackage = blockedPackage,
                    onReturnHome = { returnToHomeScreen() }
                )
            }
        }
    }

    private fun returnToHomeScreen() {
        val homeIntent = Intent(Intent.ACTION_MAIN).apply {
            addCategory(Intent.CATEGORY_HOME)
            flags = Intent.FLAG_ACTIVITY_NEW_TASK
        }
        startActivity(homeIntent)
        finish()
    }
}
