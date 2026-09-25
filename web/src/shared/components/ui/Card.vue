<script setup lang="ts">
/**
 * Card —— 结构分层（不是"白底 + 灰边"）
 *
 * 分层三件套：
 *   1. 发丝高光顶边（表面的"受光边"）
 *   2. 环境阴影（大扩散、绿相墨色）
 *   3. 可选双圈套壳：外壳做 1px 阶梯 + 内芯同心圆角
 *
 * 没有任何 1px 灰色实线描边。
 */
withDefaults(defineProps<{
  title?: string
  sub?: string
  pad?: boolean
  /** 双圈套壳（大容器 true；卡内分区关掉） */
  bezel?: boolean
  /** 悬停抬升 */
  hover?: boolean
  /** 顶部强调色条 */
  accentBar?: boolean
  /** 兼容旧 API：flat = 不套壳 */
  flat?: boolean
  /** 内芯是否改用凹槽底（用于表单/调试类容器） */
  well?: boolean
}>(), { pad: true, bezel: true, hover: false })
</script>

<template>
  <section class="shell" :class="{ bezel: bezel && !flat, hover }">
    <div class="core" :class="{ accentBar, well }">
      <header v-if="title || $slots.extra" class="head">
        <div class="hleft">
          <h3 v-if="title" class="t">{{ title }}</h3>
          <p v-if="sub" class="s">{{ sub }}</p>
        </div>
        <div v-if="$slots.extra" class="extra"><slot name="extra" /></div>
      </header>
      <div :class="{ body: pad }"><slot /></div>
    </div>
  </section>
</template>

<style scoped>
/* 外壳：极淡阶梯 + 同心大圆角，靠"台阶"而不是描边分层 */
.shell {
  position: relative;
  background: rgb(255 255 255 / .5);
  border-radius: var(--r-xl);
  padding: var(--shell-pad);
  box-shadow:
    0 1px 1px rgb(17 25 23 / .03),
    0 0 0 1px rgb(17 25 23 / .035);
  transition: box-shadow var(--t-base), transform var(--t-base);
}
.shell.hover:hover {
  box-shadow:
    0 1px 1px rgb(17 25 23 / .03),
    0 0 0 1px rgb(17 25 23 / .05),
    var(--shadow-float);
  transform: translateY(-2px);
}

/* 无套壳：直接一块抬起白面 */
.shell:not(.bezel) {
  background: transparent;
  padding: 0;
  box-shadow: none;
}

/* 内芯：白色抬起面 */
.core {
  position: relative;
  background: var(--surface);
  border-radius: var(--r-inner);
  box-shadow: var(--inset-hi);
  overflow: hidden;
}
.core.well { background: var(--well); box-shadow: none; }
.shell:not(.bezel) .core {
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-card), var(--inset-hi);
}

/* 顶部强调条：从左上淡出，不是一条平均的色块 */
.core.accentBar::before {
  content: '';
  position: absolute;
  top: 0; left: 0; right: 0;
  height: 2px;
  background: linear-gradient(90deg, var(--accent-500) 0%, var(--accent-400) 32%, transparent 88%);
  z-index: 1;
}

.head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--sp-4);
  padding: var(--sp-6) var(--sp-6) 0;
}
.hleft { min-width: 0; }
.t {
  font-size: var(--fs-md);
  font-weight: 620;
  letter-spacing: -.02em;
}
.s {
  margin-top: 3px;
  font-size: var(--fs-xs);
  color: var(--ink-500);
  line-height: 1.5;
}
.extra { display: flex; align-items: center; gap: var(--sp-2); flex: none; }
.body { padding: var(--sp-5) var(--sp-6) var(--sp-6); }
</style>
