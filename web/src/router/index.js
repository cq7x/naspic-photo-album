import { createRouter, createWebHashHistory } from 'vue-router'
import http from '../api'

const Gallery = () => import('../views/Gallery.vue')

const routes = [
  { path: '/', redirect: '/photos' },
  {
    path: '/photos',
    name: 'photos',
    component: Gallery,
    meta: { title: '照片' },
  },
  {
    path: '/albums',
    name: 'albums',
    component: () => import('../views/Albums.vue'),
    meta: { title: '相册' },
  },
  {
    path: '/albums/:id',
    name: 'album-detail',
    component: Gallery,
    props: (route) => ({ albumId: Number(route.params.id) || 0 }),
    meta: { title: '相册' },
  },
  {
    path: '/settings',
    name: 'settings',
    component: () => import('../views/Settings.vue'),
    meta: { title: '设置' },
  },
  // 老入口保留跳转：存储管理已并入「设置」
  { path: '/storage', redirect: '/settings?tab=storage' },
  { path: '/favorites', redirect: '/albums' },
  {
    path: '/login',
    name: 'login',
    component: () => import('../views/Login.vue'),
    meta: { title: '登录', public: true },
  },
  {
    path: '/setup',
    name: 'setup',
    component: () => import('../views/Setup.vue'),
    meta: { title: '数据库安装', public: true },
  },
  { path: '/:pathMatch(.*)*', redirect: '/photos' },
]

const router = createRouter({
  history: createWebHashHistory(),
  routes,
})

// 登录守卫:无 token 一律回登录页;但后端未初始化则优先跳 setup
router.beforeEach(async (to) => {
  if (to.meta.public) return true
  if (!localStorage.getItem('naspic.token')) {
    // 首次部署:先查后端是否需要安装
    try {
      const st = await http.get('/setup/status')
      if (st && st.need_setup) return { path: '/setup' }
    } catch (e) {
      // status 接口不存在(已安装且未挂 setup 路由)或网络错误,降级到登录页
    }
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  return true
})

router.afterEach((to) => {
  document.title = (to.meta.title ? to.meta.title + ' · ' : '') + 'Naspic'
})

export default router
