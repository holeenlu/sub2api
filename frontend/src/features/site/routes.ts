import { docsStaticItems, legacyAppRedirects } from './docs/content/nav'
import type { RouteRecordRaw } from 'vue-router'

export const siteRoutes: RouteRecordRaw[] = [
  {
    path: '/home',
    name: 'Home',
    component: () => import('./pages/HomePage.vue'),
    meta: { domain: 'site',  requiresAuth: false, title: 'Home', titleKey: 'home.seo.title', titleStandalone: true, metaDescriptionKey: 'home.seo.description' }
  },
  {
    path: '/legal/:documentId',
    name: 'LegalDocument',
    component: () => import('./pages/LegalDocumentPage.vue'),
    meta: { domain: 'site',  requiresAuth: false, title: 'Legal Document', titleKey: 'legal.pageTitle' }
  },
{
    path: '/admin/announcements',
    name: 'AdminAnnouncements',
    component: () => import('@/features/site/pages/AnnouncementsPage.vue'),
    meta: { domain: 'site', navigation: {order: 90, icon: 'bell', labelKey: 'nav.announcements'},  shell: 'admin',
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Announcements',
      titleKey: 'admin.announcements.title',
      descriptionKey: 'admin.announcements.description'
    }
  },
...legacyAppRedirects,
  ...docsStaticItems.map((item, index): RouteRecordRaw => ({
    path: item.path,
    name: `DocsPage${index}`,
    component: () => import('@/features/site/docs/pages/DocsView.vue'),
    meta: { domain: 'site', 
      requiresAuth: false,
      title: 'API Docs',
      titleKey: item.titleKey,
      metaDescriptionKey: item.descriptionKey
    }
  })),
  {
    path: '/docs/models/:modelId',
    name: 'DocsModel',
    component: () => import('@/features/site/docs/pages/DocsView.vue'),
    meta: { domain: 'site', 
      requiresAuth: false,
      title: 'Model - API Docs',
      titleKey: 'docs.pages.models.title',
      metaDescriptionKey: 'docs.pages.models.description'
    }
  }
]
