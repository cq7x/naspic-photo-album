import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'

import './styles/theme.css'
import './styles/element.css'
import App from './App.vue'
import router from './router'

createApp(App).use(ElementPlus, { locale: zhCn }).use(router).mount('#app')
