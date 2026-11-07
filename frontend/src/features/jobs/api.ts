/** Public batch-image wire API. Uses only the caller's explicit generation API Key. */
export {
  BatchImageApiError,
  submitBatchImageJob,
  getBatchImageJob,
  listBatchImageJobs,
  listBatchImageModels,
  listBatchImageItems,
  cancelBatchImageJob,
  downloadBatchImageZip,
  getBatchImageItemContent,
  deleteBatchImageJobRecord,
  saveBlob
} from './api/batchImage'
export type {
  BatchImageStatus,
  BatchImageSubmitItem,
  BatchImageReferenceImage,
  BatchImageSubmitRequest,
  BatchImageJob,
  BatchImageItem,
  BatchImageItemsResponse,
  BatchImageJobsResponse,
  BatchImageModel,
  BatchImageModelsResponse,
  BatchImageJobsListOptions
} from './api/batchImage'
