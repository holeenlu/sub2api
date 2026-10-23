import { apiClient } from '../client'
import type { ExcelBPSDefaults } from '@/utils/excelBPSDefaults'

export async function getExcelBPSDefaults(): Promise<ExcelBPSDefaults> {
  const { data } = await apiClient.get<ExcelBPSDefaults>('/admin/settings/excel-bps-defaults')
  return data
}
export async function saveExcelBPSDefaults(value: ExcelBPSDefaults): Promise<ExcelBPSDefaults> {
  const { data } = await apiClient.put<ExcelBPSDefaults>('/admin/settings/excel-bps-defaults', value)
  return data
}
