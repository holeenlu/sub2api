<template>
    <BaseDialog
      :show="true"
      :title="
        compositeRoutesGroup
          ? t('admin.groups.compositeRoutes.titleWithGroup', {
              name: compositeRoutesGroup.name,
            })
          : t('admin.groups.compositeRoutes.title')
      "
      width="wide"
      @close="() => emit('close')"
    >
      <div class="grid gap-5 lg:grid-cols-[minmax(0,1.15fr)_minmax(320px,0.85fr)]">
        <section class="min-w-0">
          <div class="mb-3 flex items-center justify-between gap-3">
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t("admin.groups.compositeRoutes.routes") }}
            </h3>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="compositeRoutesLoading"
              @click="loadCompositeRoutes"
            >
              <Icon
                name="refresh"
                size="sm"
                :class="compositeRoutesLoading ? 'animate-spin' : ''"
              />
            </button>
          </div>

          <div
            class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600"
          >
            <div
              v-if="compositeRoutesLoading"
              class="flex h-36 items-center justify-center text-sm text-gray-500 dark:text-gray-400"
            >
              {{ t("common.loading") }}
            </div>
            <div
              v-else-if="compositeRoutes.length === 0"
              class="flex h-36 items-center justify-center text-sm text-gray-500 dark:text-gray-400"
            >
              {{ t("admin.groups.compositeRoutes.empty") }}
            </div>
            <div v-else class="overflow-x-auto">
              <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-600">
                <thead class="bg-gray-50 text-left text-xs font-medium uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-gray-400">
                  <tr>
                    <th class="px-3 py-2">
                      {{ t("admin.groups.compositeRoutes.publicModel") }}
                    </th>
                    <th class="px-3 py-2">
                      {{ t("admin.groups.compositeRoutes.target") }}
                    </th>
                    <th class="px-3 py-2">
                      {{ t("admin.groups.compositeRoutes.scope") }}
                    </th>
                    <th class="px-3 py-2 text-right">
                      {{ t("admin.groups.columns.actions") }}
                    </th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-900">
                  <tr
                    v-for="route in compositeRoutes"
                    :key="route.id"
                    :class="!route.enabled && 'opacity-60'"
                  >
                    <td class="max-w-[15rem] px-3 py-2">
                      <div class="break-all font-medium text-gray-900 dark:text-white">
                        {{ route.public_model }}
                      </div>
                      <div class="mt-1 flex flex-wrap items-center gap-1.5">
                        <span class="badge badge-gray">{{
                          compositeRouteMatchLabel(route.match_type)
                        }}</span>
                        <span
                          v-if="!route.enabled"
                          class="badge badge-danger"
                        >
                          {{ t("admin.accounts.status.inactive") }}
                        </span>
                      </div>
                    </td>
                    <td class="px-3 py-2">
                      <div class="flex items-center gap-1.5 text-gray-900 dark:text-white">
                        <PlatformIcon :platform="route.target_platform" size="xs" />
                        <span>{{ formatCompositePlatform(route.target_platform) }}</span>
                      </div>
                      <div class="mt-1 break-all text-xs text-gray-500 dark:text-gray-400">
                        {{ route.upstream_model || route.public_model }}
                      </div>
                    </td>
                    <td class="px-3 py-2">
                      <div class="text-gray-700 dark:text-gray-300">
                        {{ formatCompositeEndpoint(route.endpoint) }}
                      </div>
                      <div class="text-xs text-gray-500 dark:text-gray-400">
                        {{ t("admin.groups.compositeRoutes.priority") }}:
                        {{ route.priority }}
                      </div>
                    </td>
                    <td class="px-3 py-2">
                      <div class="flex justify-end gap-1">
                        <button v-if="rolePermissions.can('groups.manage')"
                          type="button"
                          class="rounded p-1.5 text-gray-500 hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400"
                          :title="t('common.edit')"
                          @click="editCompositeRoute(route)"
                        >
                          <Icon name="edit" size="sm" />
                        </button>
                        <button v-if="rolePermissions.can('groups.manage')"
                          type="button"
                          class="rounded p-1.5 text-gray-500 hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                          :title="t('common.delete')"
                          @click="deleteCompositeRoute(route)"
                        >
                          <Icon name="trash" size="sm" />
                        </button>
                      </div>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </section>

        <section class="space-y-5">
          <form v-if="rolePermissions.can('groups.manage')" class="space-y-3" @submit.prevent="saveCompositeRoute">
            <div class="flex items-center justify-between gap-3">
              <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
                {{
                  compositeRouteEditingId
                    ? t("admin.groups.compositeRoutes.editRoute")
                    : t("admin.groups.compositeRoutes.addRoute")
                }}
              </h3>
              <button
                v-if="compositeRouteEditingId"
                type="button"
                class="text-xs font-medium text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
                @click="resetCompositeRouteForm"
              >
                {{ t("common.cancel") }}
              </button>
            </div>

            <div>
              <label class="input-label">{{
                t("admin.groups.compositeRoutes.publicModel")
              }}</label>
              <input
                v-model.trim="compositeRouteForm.public_model"
                type="text"
                class="input"
                required
                placeholder="openrouter/gpt-5"
              />
            </div>

            <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div>
                <label class="input-label">{{
                  t("admin.groups.compositeRoutes.matchType")
                }}</label>
                <Select
                  v-model="compositeRouteForm.match_type"
                  :options="compositeRouteMatchOptions"
                />
              </div>
              <div>
                <label class="input-label">{{
                  t("admin.groups.compositeRoutes.endpoint")
                }}</label>
                <Select
                  v-model="compositeRouteForm.endpoint"
                  :options="compositeRouteEndpointOptions"
                />
              </div>
            </div>

            <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div>
                <label class="input-label">{{
                  t("admin.groups.compositeRoutes.targetPlatform")
                }}</label>
                <Select
                  v-model="compositeRouteForm.target_platform"
                  :options="compositeRoutePlatformOptions"
                />
              </div>
              <div>
                <label class="input-label">{{
                  t("admin.groups.compositeRoutes.priority")
                }}</label>
                <input
                  v-model.number="compositeRouteForm.priority"
                  type="number"
                  min="1"
                  step="1"
                  class="input"
                />
              </div>
            </div>

            <div>
              <label class="input-label">{{
                t("admin.groups.compositeRoutes.upstreamModel")
              }}</label>
              <input
                v-model.trim="compositeRouteForm.upstream_model"
                type="text"
                class="input"
                placeholder="gpt-5"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.groups.compositeRoutes.upstreamModelHint") }}
              </p>
            </div>

            <div>
              <label class="input-label">{{
                t("admin.groups.compositeRoutes.notes")
              }}</label>
              <textarea
                v-model.trim="compositeRouteForm.notes"
                rows="2"
                class="input"
              ></textarea>
            </div>

            <div class="flex items-center justify-between gap-3">
              <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
                <input
                  v-model="compositeRouteForm.enabled"
                  type="checkbox"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-700"
                />
                {{ t("admin.groups.compositeRoutes.enabled") }}
              </label>
              <button
                type="submit"
                class="btn btn-primary"
                :disabled="compositeRouteSaving"
              >
                <Icon
                  v-if="!compositeRouteSaving"
                  name="check"
                  size="sm"
                  class="mr-2"
                />
                {{ compositeRouteEditingId ? t("common.update") : t("common.create") }}
              </button>
            </div>
          </form>

          <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
            <h3 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">
              {{ t("admin.groups.compositeRoutes.preview") }}
            </h3>
            <div class="space-y-3">
              <input
                v-model.trim="compositePreviewModel"
                type="text"
                class="input"
                placeholder="openrouter/gpt-5"
                @keyup.enter="previewCompositeRoute"
              />
              <div class="flex gap-2">
                <Select
                  v-model="compositePreviewEndpoint"
                  :options="compositeRouteEndpointOptions"
                  class="min-w-0 flex-1"
                />
                <button
                  type="button"
                  class="btn btn-secondary"
                  :disabled="compositePreviewLoading || !compositePreviewModel"
                  @click="previewCompositeRoute"
                >
                  <Icon name="play" size="sm" />
                </button>
              </div>

              <div
                v-if="compositePreviewDecision"
                class="rounded-lg border border-gray-200 bg-gray-50 p-3 text-sm dark:border-dark-600 dark:bg-dark-800"
              >
                <div class="mb-2 flex items-center gap-2">
                  <span
                    :class="[
                      'badge',
                      compositePreviewDecision.matched
                        ? 'badge-success'
                        : 'badge-danger',
                    ]"
                  >
                    {{
                      compositePreviewDecision.matched
                        ? t("admin.groups.compositeRoutes.matched")
                        : t("admin.groups.compositeRoutes.notMatched")
                    }}
                  </span>
                  <span class="badge badge-gray">
                    {{
                      compositeRouteSourceLabel(
                        compositePreviewDecision.source,
                      )
                    }}
                  </span>
                </div>
                <div
                  v-if="compositePreviewDecision.matched"
                  class="space-y-1 text-gray-700 dark:text-gray-300"
                >
                  <div>
                    {{ t("admin.groups.compositeRoutes.targetPlatform") }}:
                    {{
                      formatCompositePlatform(
                        compositePreviewDecision.target_platform,
                      )
                    }}
                  </div>
                  <div class="break-all">
                    {{ t("admin.groups.compositeRoutes.upstreamModel") }}:
                    {{ compositePreviewDecision.upstream_model }}
                  </div>
                </div>
                <div
                  v-else
                  class="text-gray-500 dark:text-gray-400"
                >
                  {{ compositePreviewDecision.reason }}
                </div>
              </div>
            </div>
          </div>
        </section>
      </div>

      <template #footer>
        <div class="flex justify-end pt-4">
          <button
            type="button"
            class="btn btn-secondary"
            @click="() => emit('close')"
          >
            {{ t("common.close") }}
          </button>
        </div>
      </template>
    </BaseDialog>
</template>
<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import type { AdminGroup, GroupPlatform, CompositeModelRoute, CompositeModelRouteInput, CompositeRouteDecision, CompositeRouteEndpoint, CompositeRouteMatchType } from '@/features/access-policy/api/groupTypes';
import { groupsAPI } from '../api/groups';
import { accessPolicyNotificationsKey, accessPolicyPermissionsKey, requireAccessPolicyService } from '../services';
import { useAccessPolicyRequest } from '../queries/requestScope';
import { useGroupPresentation } from '@/features/access-policy/presentation/context'
const { platformLabel, concretePlatformOptions } = useGroupPresentation();
import BaseDialog from '@/shared/ui/BaseDialog.vue';

import Select from '@/shared/ui/Select.vue';

import Icon from '@/shared/ui/icons/Icon.vue';

import PlatformIcon from '@/shared/ui/PlatformIcon.vue';


const { t } = useI18n()
const appStore = requireAccessPolicyService(accessPolicyNotificationsKey)
const rolePermissions = requireAccessPolicyService(accessPolicyPermissionsKey)
const emit = defineEmits<{ close: []; saved: [] }>()
type ConcreteGroupPlatform = Exclude<GroupPlatform, "composite">;
type CompositeRouteFormState = {
  public_model: string;
  match_type: CompositeRouteMatchType;
  target_platform: ConcreteGroupPlatform;
  upstream_model: string;
  endpoint: CompositeRouteEndpoint;
  priority: number;
  enabled: boolean;
  notes: string;
};
const compositeRoutePlatformOptions = computed(() => [
  ...concretePlatformOptions.value,
]);
const compositeRouteEndpointOptions = computed(() => [
  { value: "any", label: t("admin.groups.compositeRoutes.endpoints.any") },
  {
    value: "messages",
    label: t("admin.groups.compositeRoutes.endpoints.messages"),
  },
  {
    value: "count_tokens",
    label: t("admin.groups.compositeRoutes.endpoints.countTokens"),
  },
  {
    value: "responses",
    label: t("admin.groups.compositeRoutes.endpoints.responses"),
  },
  {
    value: "chat_completions",
    label: t("admin.groups.compositeRoutes.endpoints.chatCompletions"),
  },
  {
    value: "embeddings",
    label: t("admin.groups.compositeRoutes.endpoints.embeddings"),
  },
  { value: "images", label: t("admin.groups.compositeRoutes.endpoints.images") },
  { value: "gemini", label: t("admin.groups.compositeRoutes.endpoints.gemini") },
]);
const compositeRouteMatchOptions = computed(() => [
  { value: "exact", label: t("admin.groups.compositeRoutes.match.exact") },
  { value: "prefix", label: t("admin.groups.compositeRoutes.match.prefix") },
]);
const compositeRoutes = ref<CompositeModelRoute[]>([]);
const compositeRoutesLoading = ref(false);
const compositeRouteSaving = ref(false);
const compositeRouteEditingId = ref<number | null>(null);
const compositePreviewModel = ref("");
const compositePreviewEndpoint = ref<CompositeRouteEndpoint>("any");
const compositePreviewLoading = ref(false);
const compositePreviewDecision = ref<CompositeRouteDecision | null>(null);
const compositeRouteForm = reactive<CompositeRouteFormState>({
  public_model: "",
  match_type: "exact",
  target_platform: "openai",
  upstream_model: "",
  endpoint: "any",
  priority: 100,
  enabled: true,
  notes: "",
});
const compositeRouteMatchLabel = (matchType: CompositeRouteMatchType) =>
  compositeRouteMatchOptions.value.find((option) => option.value === matchType)
    ?.label || matchType;
const formatCompositeEndpoint = (endpoint: CompositeRouteEndpoint) =>
  compositeRouteEndpointOptions.value.find((option) => option.value === endpoint)
    ?.label || endpoint;
const formatCompositePlatform = (platform: string) => {
  if (!platform) return "—";
  return t(`admin.groups.platforms.${platform}`, platformLabel(platform));
};
const compositeRouteSourceLabel = (source: string) => {
  if (source === "route") return t("admin.groups.compositeRoutes.sources.route");
  if (source === "detector") {
    return t("admin.groups.compositeRoutes.sources.detector");
  }
  return source || "—";
};
const resetCompositeRouteForm = () => {
  compositeRouteEditingId.value = null;
  compositeRouteForm.public_model = "";
  compositeRouteForm.match_type = "exact";
  compositeRouteForm.target_platform = "openai";
  compositeRouteForm.upstream_model = "";
  compositeRouteForm.endpoint = "any";
  compositeRouteForm.priority = 100;
  compositeRouteForm.enabled = true;
  compositeRouteForm.notes = "";
};
const toCompositeRouteInput = (): CompositeModelRouteInput => ({
  public_model: compositeRouteForm.public_model.trim(),
  match_type: compositeRouteForm.match_type,
  target_platform: compositeRouteForm.target_platform,
  upstream_model: compositeRouteForm.upstream_model.trim(),
  endpoint: compositeRouteForm.endpoint,
  priority: String(compositeRouteForm.priority).trim() !== "" && Number.isFinite(Number(compositeRouteForm.priority)) ? Number(compositeRouteForm.priority) : 100,
  enabled: compositeRouteForm.enabled,
  notes: compositeRouteForm.notes.trim(),
});
const loadCompositeRoutes = async () => {
  if (invalidated.value) return;
  const request = listOwner.begin();
  if (!compositeRoutesGroup.value) return;
  compositeRoutesLoading.value = true;
  try {
    const routes = await groupsAPI.listCompositeRoutes(
      compositeRoutesGroup.value.id,
    );
    if (!request.isCurrent()) return;
    compositeRoutes.value = [...routes].sort((a, b) => {
      if (a.priority !== b.priority) return a.priority - b.priority;
      return a.id - b.id;
    });
  } catch (error: unknown) {
    if (!request.isCurrent()) return;
    appStore.showError(
      (error as { response?: { data?: { detail?: string } } }).response?.data?.detail ||
        (error as { response?: { data?: { message?: string } } }).response?.data?.message ||
        t("admin.groups.compositeRoutes.failedToLoad"),
    );
    console.error("Error loading composite routes:", error);
  } finally {
    if (request.isCurrent()) compositeRoutesLoading.value = false;
  }
};
const editCompositeRoute = (route: CompositeModelRoute) => {
  compositeRouteEditingId.value = route.id;
  compositeRouteForm.public_model = route.public_model;
  compositeRouteForm.match_type = route.match_type;
  compositeRouteForm.target_platform = route.target_platform;
  compositeRouteForm.upstream_model = route.upstream_model;
  compositeRouteForm.endpoint = route.endpoint;
  compositeRouteForm.priority = route.priority ?? 100;
  compositeRouteForm.enabled = route.enabled;
  compositeRouteForm.notes = route.notes || "";
};
const saveCompositeRoute = async () => {
  if (invalidated.value || compositeRouteSaving.value || !rolePermissions.can('groups.manage')) return;
  const request = saveOwner.begin();
  if (!compositeRoutesGroup.value) return;
  if (!compositeRouteForm.public_model.trim()) {
    appStore.showError(t("admin.groups.compositeRoutes.publicModelRequired"));
    return;
  }
  compositeRouteSaving.value = true;
  try {
    const payload = toCompositeRouteInput();
    if (compositeRouteEditingId.value) {
      await groupsAPI.updateCompositeRoute(
        compositeRoutesGroup.value.id,
        compositeRouteEditingId.value,
        payload,
      );
      if (!request.isCurrent()) return;
    appStore.showSuccess(t("admin.groups.compositeRoutes.routeUpdated"));
    } else {
      await groupsAPI.createCompositeRoute(
        compositeRoutesGroup.value.id,
        payload,
      );
      if (!request.isCurrent()) return;
    appStore.showSuccess(t("admin.groups.compositeRoutes.routeCreated"));
    }
    resetCompositeRouteForm();
    await loadCompositeRoutes();
  } catch (error: unknown) {
    if (!request.isCurrent()) return;
    appStore.showError(
      (error as { response?: { data?: { detail?: string } } }).response?.data?.detail ||
        (error as { response?: { data?: { message?: string } } }).response?.data?.message ||
        t("admin.groups.compositeRoutes.failedToSave"),
    );
    console.error("Error saving composite route:", error);
  } finally {
    if (request.isCurrent()) compositeRouteSaving.value = false;
  }
};
const deleteCompositeRoute = async (route: CompositeModelRoute) => {
  if (invalidated.value || compositeRouteSaving.value || !rolePermissions.can('groups.manage')) return;
  const request = saveOwner.begin();
  if (!compositeRoutesGroup.value) return;
  if (!window.confirm(t("admin.groups.compositeRoutes.deleteConfirm"))) return;
  compositeRouteSaving.value = true;
  try {
    await groupsAPI.deleteCompositeRoute(
      compositeRoutesGroup.value.id,
      route.id,
    );
    if (!request.isCurrent()) return;
    if (compositeRouteEditingId.value === route.id) {
      resetCompositeRouteForm();
    }
    if (!request.isCurrent()) return;
    appStore.showSuccess(t("admin.groups.compositeRoutes.routeDeleted"));
    await loadCompositeRoutes();
  } catch (error: unknown) {
    if (!request.isCurrent()) return;
    appStore.showError(
      (error as { response?: { data?: { detail?: string } } }).response?.data?.detail ||
        (error as { response?: { data?: { message?: string } } }).response?.data?.message ||
        t("admin.groups.compositeRoutes.failedToDelete"),
    );
    console.error("Error deleting composite route:", error);
  } finally {
    if (request.isCurrent()) compositeRouteSaving.value = false;
  }
};
const previewCompositeRoute = async () => {
  if (invalidated.value) return;
  const request = previewOwner.begin();
  if (!compositeRoutesGroup.value || !compositePreviewModel.value.trim()) {
    return;
  }
  compositePreviewLoading.value = true;
  try {
    const decision = await groupsAPI.previewCompositeRoute(
      compositeRoutesGroup.value.id,
      {
        model: compositePreviewModel.value.trim(),
        endpoint: compositePreviewEndpoint.value,
      },
    );
    if (request.isCurrent()) compositePreviewDecision.value = decision;
  } catch (error: unknown) {
    if (!request.isCurrent()) return;
    appStore.showError(
      (error as { response?: { data?: { detail?: string } } }).response?.data?.detail ||
        (error as { response?: { data?: { message?: string } } }).response?.data?.message ||
        t("admin.groups.compositeRoutes.failedToPreview"),
    );
    console.error("Error previewing composite route:", error);
  } finally {
    if (request.isCurrent()) compositePreviewLoading.value = false;
  }
};
const props = defineProps<{ group: AdminGroup }>()
const compositeRoutesGroup = computed(() => props.group)
const invalidated = ref(false)
const listOwner = useAccessPolicyRequest(() => { compositeRoutes.value = []; compositeRoutesLoading.value = false })
const saveOwner = useAccessPolicyRequest(() => { compositeRouteSaving.value = false })
const previewOwner = useAccessPolicyRequest(() => { compositePreviewDecision.value = null; compositePreviewLoading.value = false })
const owner = useAccessPolicyRequest(() => {
 invalidated.value = true
 listOwner.cancel(); saveOwner.cancel(); previewOwner.cancel()
 resetCompositeRouteForm()
 emit('close')
})
watch([() => props.group.id, () => rolePermissions.can('groups.manage')], () => owner.cancel(), { flush: 'sync' })
watch([compositePreviewModel, compositePreviewEndpoint], () => previewOwner.cancel(), { flush: 'sync' })

onMounted(loadCompositeRoutes)

</script>
