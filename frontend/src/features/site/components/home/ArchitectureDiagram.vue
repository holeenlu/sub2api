<template>
  <!--
    三层能力架构图。
    编号 ①-⑦ 是能力索引而非请求步骤：③ 嵌在 ② 之内，④ 是条件分支。
    文字全部放在 HTML 节点里，连接线用 CSS 边框绘制，行动版只去掉跨卡长线，层级不变。
  -->
  <div
    data-testid="architecture-diagram"
    class="rounded-2xl border border-gray-200/70 bg-white/80 p-3.5 shadow-sm backdrop-blur-sm sm:p-6 dark:border-dark-700/70 dark:bg-dark-900/70"
  >
    <!-- 接入端 -->
    <div :class="bandClass">
      <span :class="bandLabelClass">{{ t('home.architecture.layers.access') }}</span>
      <div class="grid grid-cols-1 gap-3 md:grid-cols-[1fr_auto_1fr_1fr] md:items-center">
        <DiagramNode
          badge="①"
          :title="t('home.mechanisms.marketplace.title')"
          :english="englishTitle('marketplace')"
          :sub="t('home.architecture.nodes.marketplaceDesc')"
          accent
        />
        <div :class="connHClass">
          <span :class="connLabelClass">{{ t('home.architecture.edges.selection') }}</span>
          <span class="flex w-full min-w-[56px] items-center max-md:hidden">
            <span class="flex-1 border-t-2 border-dashed border-gray-200 dark:border-dark-600"></span>
            <span :class="arrowRightClass"></span>
          </span>
          <span :class="arrowDownClass" class="md:hidden"></span>
        </div>
        <DiagramNode
          badge="·"
          plain
          :title="t('home.architecture.nodes.application')"
          :sub="t('home.architecture.nodes.applicationDesc')"
        />
        <DiagramNode
          badge="⑦"
          :title="t('home.mechanisms.agents.title')"
          :english="englishTitle('agents')"
          :sub="t('home.architecture.nodes.agentsDesc')"
          accent
        />
      </div>
    </div>

    <!-- 接入端 → 请求处理 -->
    <div class="flex items-stretch gap-4 px-2 py-2 md:grid md:grid-cols-[1fr_auto_1fr_1fr]">
      <div :class="connVClass" class="md:col-start-3">
        <span :class="stemClass"></span>
        <span :class="connLabelClass">{{ t('home.architecture.edges.request') }}</span>
        <span :class="stemClass"></span>
        <span :class="arrowDownClass"></span>
      </div>
      <div :class="connVClass">
        <span :class="stemClass"></span>
        <span :class="connLabelClass">{{ t('home.architecture.edges.request') }}</span>
        <span :class="stemClass"></span>
        <span :class="arrowDownClass"></span>
      </div>
    </div>

    <!-- 请求处理 -->
    <div :class="bandClass">
      <span :class="bandLabelClass">{{ t('home.architecture.layers.processing') }}</span>
      <div class="grid grid-cols-1 gap-3 md:grid-cols-[1fr_auto_1fr] md:items-start">
        <DiagramNode
          data-testid="node-routing"
          badge="②"
          :title="t('home.mechanisms.routing.title')"
          :english="englishTitle('routing')"
          :sub="t('home.mechanisms.routing.description')"
          accent
        >
          <!-- ③ 嵌入 ②：成本因子不是必经步骤，所以不另画节点 -->
          <div
            data-testid="node-cost"
            class="mt-3 rounded-xl border border-dashed border-primary-200 bg-primary-50/70 px-3.5 py-3 dark:border-primary-800 dark:bg-primary-950/30"
          >
            <h4 class="flex items-center gap-2 text-[13.5px] font-semibold text-gray-900 dark:text-white">
              <span :class="badgeClass">③</span>
              {{ t('home.mechanisms.cost.title') }}
            </h4>
            <p v-if="showEnglish" class="ml-[30px] mt-0.5 text-[11.5px] text-gray-400 dark:text-dark-500">
              {{ englishTitle('cost') }}
            </p>
            <p class="mt-2 text-[11.5px] leading-relaxed text-gray-500 dark:text-dark-400">
              {{ t('home.mechanisms.cost.note') }}
            </p>
          </div>
        </DiagramNode>

        <div :class="connHClass">
          <span class="flex w-full min-w-[56px] items-center max-md:hidden">
            <span class="flex-1 border-t-2 border-gray-200 dark:border-dark-600"></span>
            <span :class="arrowRightClass"></span>
          </span>
          <span :class="arrowDownClass" class="md:hidden"></span>
        </div>

        <div class="flex flex-col">
          <DiagramNode
            badge="·"
            plain
            :title="t('home.architecture.nodes.upstream')"
            :sub="t('home.architecture.nodes.upstreamDesc')"
          />
          <div :class="connVClass">
            <span :class="stemClass" class="border-dashed !border-amber-400"></span>
            <span :class="connLabelClass">{{ t('home.architecture.edges.error') }}</span>
            <span :class="stemClass" class="border-dashed !border-amber-400"></span>
            <span
              class="h-0 w-0 border-x-[5px] border-t-[7px] border-x-transparent border-t-amber-500"
            ></span>
          </div>
          <DiagramNode
            badge="④"
            branch
            :title="t('home.mechanisms.failover.title')"
            :english="englishTitle('failover')"
            :sub="t('home.architecture.nodes.failoverDesc')"
          />
        </div>
      </div>

      <!-- ④ → ② 回接 -->
      <div
        class="mt-3.5 flex items-center gap-2.5 rounded-xl border border-dashed border-amber-200 bg-amber-50/70 px-3 py-2 dark:border-amber-800/60 dark:bg-amber-950/20"
      >
        <span class="h-0 w-0 border-y-[5px] border-r-[7px] border-y-transparent border-r-amber-500"></span>
        <span class="flex-1 border-t-2 border-dashed border-amber-300 dark:border-amber-700"></span>
        <span class="whitespace-nowrap text-[11px] text-amber-700 dark:text-amber-400">
          {{ t('home.architecture.edges.retry') }} → ② {{ t('home.mechanisms.routing.title') }}
        </span>
      </div>
    </div>

    <!-- 请求处理 → 管理层 -->
    <div class="flex items-stretch gap-4 px-2 py-2">
      <div class="flex flex-1 flex-col items-center justify-center">
        <span
          class="h-3 w-full rounded-t-lg border-2 border-b-0 border-dashed border-gray-200 dark:border-dark-600"
        ></span>
        <span :class="connLabelClass" class="mt-1.5">{{ t('home.architecture.edges.controls') }}</span>
      </div>
      <div :class="connVClass">
        <span :class="stemClass" class="border-dashed"></span>
        <span :class="connLabelClass">{{ t('home.architecture.edges.usage') }}</span>
        <span :class="stemClass" class="border-dashed"></span>
        <span :class="arrowDownClass"></span>
      </div>
    </div>

    <!-- 管理层 -->
    <div :class="bandClass">
      <span :class="bandLabelClass">{{ t('home.architecture.layers.management') }}</span>
      <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
        <DiagramNode
          badge="⑥"
          accent
          :title="t('home.mechanisms.gateway.title')"
          :english="englishTitle('gateway')"
          :sub="t('home.architecture.nodes.gatewayDesc')"
        />
        <DiagramNode
          badge="⑤"
          accent
          :title="t('home.mechanisms.billing.title')"
          :english="englishTitle('billing')"
          :sub="t('home.architecture.nodes.billingDesc')"
        />
      </div>
    </div>

    <p class="mt-4 text-center text-[13px] text-gray-500 dark:text-dark-400">
      {{ t('home.architecture.indexNote') }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import DiagramNode from './DiagramNode.vue'
import { useMechanismEnglishTitle, BADGE_CLASS } from './mechanisms'

const { t } = useI18n()
const { showEnglish, englishTitle } = useMechanismEnglishTitle()

const badgeClass = BADGE_CLASS

const bandClass =
  'rounded-2xl border border-gray-200/80 bg-gradient-to-b from-white to-gray-50/80 p-3.5 sm:p-4 dark:border-dark-700/80 dark:from-dark-800/60 dark:to-dark-900/60'
const bandLabelClass =
  'mb-3.5 inline-block rounded-md border border-primary-100 bg-primary-50 px-2.5 py-[3px] text-[11px] font-bold tracking-[0.12em] text-primary-700 dark:border-primary-900 dark:bg-primary-950/40 dark:text-primary-300'
const connVClass = 'flex min-h-[56px] flex-1 flex-col items-center justify-center'
const connHClass = 'flex flex-col items-center justify-center gap-1.5 px-1 py-2 md:py-0'
const connLabelClass =
  'rounded-full border border-gray-200 bg-white px-2.5 py-0.5 text-center text-[11px] text-gray-500 dark:border-dark-600 dark:bg-dark-800 dark:text-dark-400'
const stemClass = 'my-0.5 h-3 w-0 border-l-2 border-gray-200 dark:border-dark-600'
const arrowDownClass =
  'h-0 w-0 border-x-[5px] border-t-[7px] border-x-transparent border-t-gray-300 dark:border-t-dark-600'
const arrowRightClass =
  'h-0 w-0 border-y-[5px] border-l-[7px] border-y-transparent border-l-gray-300 dark:border-l-dark-600'
</script>
