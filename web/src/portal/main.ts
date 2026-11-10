import { createApp } from 'vue'
import '@fontsource-variable/jetbrains-mono'
import '../shared/styles/tokens.css'
import './theme.css'
import App from './App.vue'

document.documentElement.classList.add('portal-theme')
createApp(App).mount('#app')