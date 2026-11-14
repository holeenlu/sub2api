import type { RouteRecordRaw } from 'vue-router'
export const paymentsRoutes: RouteRecordRaw[] = [
{
    path: '/purchase',
    name: 'PurchaseSubscription',
    component: () => import('./pages/CheckoutPage.vue'),
    meta: { domain: 'payments', navigation: {order: 80, icon: 'rechargeSubscription', hideInSimpleMode: true, feature: 'payment'},  requiresAuth: true, requiresAdmin: false, title: 'Purchase Subscription', titleKey: 'nav.buySubscription', descriptionKey: 'purchase.description', requiresPayment: true, shell: 'user' }
  },
{
    path: '/auth/wechat/payment/callback',
    name: 'WeChatPaymentOAuthCallback',
    component: () => import('./pages/WechatPaymentCallbackPage.vue'),
    meta: { domain: 'payments',  requiresAuth: false, title: 'WeChat Payment Callback', titleKey: 'auth.wechatPaymentCallbackPageTitle' }
  },
{
    path: '/orders',
    name: 'OrderList',
    component: () => import('./pages/UserOrdersPage.vue'),
    meta: { domain: 'payments', navigation: {order: 90, icon: 'orderList', hideInSimpleMode: true, feature: 'payment'},  shell: 'user',
      requiresAuth: true,
      requiresAdmin: false,
      title: 'My Orders',
      titleKey: 'nav.myOrders',
      requiresPayment: true
    }
  },
{
    path: '/payment/qrcode',
    name: 'PaymentQRCode',
    component: () => import('./pages/PaymentQRCodePage.vue'),
    meta: { domain: 'payments',  shell: 'user',
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Payment',
      titleKey: 'payment.qr.scanToPay',
      requiresPayment: true
    }
  },
{
    path: '/payment/result',
    name: 'PaymentResult',
    component: () => import('./pages/PaymentResultPage.vue'),
    meta: { domain: 'payments', 
      requiresAuth: false,
      requiresAdmin: false,
      title: 'Payment Result',
      titleKey: 'payment.result.success',
      requiresPayment: false
    }
  },
{
    path: '/payment/stripe',
    name: 'StripePayment',
    component: () => import('./pages/StripePaymentPage.vue'),
    meta: { domain: 'payments', 
      requiresAuth: false,
      requiresAdmin: false,
      title: 'Stripe Payment',
      titleKey: 'payment.stripePay',
      requiresPayment: false
    }
  },
{
    path: '/payment/airwallex',
    name: 'AirwallexPayment',
    component: () => import('./pages/AirwallexPaymentPage.vue'),
    meta: { domain: 'payments',  shell: 'user',
      requiresAuth: false,
      requiresAdmin: false,
      title: 'Airwallex Payment',
      titleKey: 'payment.airwallexPay',
      requiresPayment: false
    }
  },
{
    path: '/payment/stripe-popup',
    name: 'StripePopup',
    component: () => import('./pages/StripePopupPage.vue'),
    meta: { domain: 'payments', 
      requiresAuth: false,
      requiresAdmin: false,
      title: 'Payment',
      titleKey: 'payment.stripePay',
      requiresPayment: false
    }
  },
{
    path: '/admin/orders/dashboard',
    name: 'AdminPaymentDashboard',
    component: () => import('./pages/AdminPaymentDashboardPage.vue'),
    meta: { domain: 'payments', navigation: {order: 10, group: 'orders', icon: 'chart'},  shell: 'admin',
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Payment Dashboard',
      titleKey: 'nav.paymentDashboard',
      requiresPayment: true
    }
  },
{
    path: '/admin/orders',
    name: 'AdminOrders',
    component: () => import('./pages/AdminOrdersPage.vue'),
    meta: { domain: 'payments', navigation: {order: 20, group: 'orders', icon: 'order'}, navigationGroup: {id: 'orders', labelKey: 'nav.orderManagement', icon: 'order', order: 120, surface: 'admin', feature: 'adminPayment', hideInSimpleMode: true},  shell: 'admin',
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Order Management',
      titleKey: 'nav.orderManagement',
      requiresPayment: true
    }
  },
{
    path: '/admin/orders/plans',
    name: 'AdminPaymentPlans',
    component: () => import('./pages/AdminPaymentPlansPage.vue'),
    meta: { domain: 'payments', navigation: {order: 30, group: 'orders', icon: 'creditCard'},  shell: 'admin',
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Subscription Plans',
      titleKey: 'nav.paymentPlans',
      requiresPayment: true
    }
  }
]
