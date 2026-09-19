import { createRouter, createWebHashHistory } from 'vue-router'

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
  { path: '/:pathMatch(.*)*', redirect: '/photos' },
]

const router = createRouter({
  history: createWebHashHistory(),
  routes,
})

// 登录守卫：无 token 一律回登录页
router.beforeEach((to) => {
  if (to.meta.public) return true
  if (!localStorage.getItem('naspic.token')) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  return true
})

router.afterEach((to) => {
  document.title = (to.meta.title ? to.meta.title + ' · ' : '') + 'Naspic'
})

export default router
