import { reactive } from 'vue'

/** Portal 跨视图共享状态 */
export const portalStore = reactive({
  /** 试一发预选模型（购物卡「试用」→ Console 自动选中） */
  tryModel: '',
})