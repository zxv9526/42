<template>
  <!-- Integrated Single-Layer Settlement Overlay -->
  <div
    v-if="hasWon"
    class="absolute inset-0 z-40 flex items-center justify-center p-1 sm:p-2 select-none bg-black/75 backdrop-blur-md overflow-hidden"
  >
    <div
      class="w-full max-w-4xl max-h-[98%] sm:max-h-[96%] rounded-xl sm:rounded-2xl border-2 border-[#C89B3C] shadow-[0_20px_60px_rgba(30,20,10,0.65),0_0_35px_rgba(200,155,60,0.25)] flex flex-col justify-between p-2 sm:p-2.5 gap-1 sm:gap-1.5 overflow-hidden text-[#3C2C1E] my-auto relative"
      :style="cardPaperStyle"
    >
      <!-- 1. Header -->
      <div class="flex items-center justify-between gap-2 pb-1 border-b border-[#D8C7B0] shrink-0">
        <div class="flex items-center gap-2 text-left">
          <span class="text-2xl sm:text-3xl filter drop-shadow-xs">
            {{ winningBreakdown.winnerSeat === 0 ? '🏆' : (winningBreakdown.rewardCoins > 0 ? '💰' : '🍂') }}
          </span>
          <div>
            <div class="flex items-center gap-1.5">
              <h3 class="text-sm sm:text-base md:text-lg font-black text-[#8B2516] font-serif-title tracking-wide leading-none">
                {{ winningBreakdown.isFourHongzhong
                  ? '四张红中 · 天降大满贯！'
                  : (winningBreakdown.winnerSeat === 0 ? '自摸和牌 · 雀圣大捷！' : `【${settledWinnerLabel}】自摸和牌`) }}
              </h3>
              <span
                class="px-1.5 py-0.2 rounded text-[10px] sm:text-xs font-bold leading-none"
                :class="winningBreakdown.winnerSeat === 0 ? 'bg-[#FAF0E2] text-[#8B2516] border border-[#C89B3C]/70' : 'bg-[#E5D7C2] text-[#5C4533] border border-[#D8C7B0]'"
              >
                {{ winningBreakdown.winnerSeat === 0 ? '本席自摸' : '对手自摸' }}
              </span>
            </div>
            <p class="text-[11px] sm:text-xs text-[#6B5744] font-medium mt-0.5 leading-tight">
              底分 10 分 · 下局由自摸者【{{ settledWinnerLabel }}】坐庄率先出牌
            </p>
          </div>
        </div>

        <div class="flex items-center gap-1.5 sm:gap-2.5 shrink-0">
          <div class="px-2 py-0.5 sm:py-1 rounded-lg bg-[#E6F4EA] border border-[#34A853]/60 text-[#137333] flex items-center gap-1 shadow-xs font-bold">
            <span class="text-[11px] sm:text-xs">🎯 中码:</span>
            <span class="text-xs sm:text-sm font-mono font-black text-[#137333]">
              {{ winningBreakdown.hitMaCount }} / {{ winningBreakdown.maCount }} 码
            </span>
          </div>

          <div
            class="px-2.5 sm:px-3 py-0.5 sm:py-1 rounded-lg flex items-center gap-1 shadow-xs"
            :class="winningBreakdown.rewardCoins >= 0
              ? 'bg-[#FEF7E0] border border-[#F9AB00] text-[#B06000]'
              : 'bg-[#FCE8E6] border border-[#EA4335] text-[#C5221F]'"
          >
            <span class="text-[11px] sm:text-xs font-bold">🪙 {{ winningBreakdown.rewardCoins >= 0 ? '赢得:' : '扣除:' }}</span>
            <span
              class="text-sm sm:text-base font-mono font-black tabular-nums"
              :class="winningBreakdown.rewardCoins >= 0 ? 'text-[#B06000]' : 'text-[#C5221F]'"
            >
              {{ winningBreakdown.rewardCoins >= 0 ? `+${winningBreakdown.rewardCoins}` : Math.abs(winningBreakdown.rewardCoins) }} 分
            </span>
            <span class="text-[10px] opacity-80 font-normal">
              ({{ winningBreakdown.payoutPerOpponent }}分/家)
            </span>
          </div>
        </div>
      </div>

      <!-- 2. Winner Hand & Melds -->
      <div class="flex flex-col gap-0.5 shrink-0 bg-[#FAF4E8]/95 rounded-lg sm:rounded-xl px-2 py-1 border border-[#D8C7B0] shadow-xs">
        <div class="flex items-center justify-between text-[11px] sm:text-xs text-[#78350F] font-bold font-serif-title leading-tight">
          <span class="flex items-center gap-1">
            <span>🀄</span>
            <span>【{{ settledWinnerLabel }}】胡牌手牌与亮副全貌</span>
          </span>
          <span class="text-[#78695C] font-mono text-[10px]">
            共 {{ settledWinningHand.length + (settledWinningMelds.length * 3) }} 张
          </span>
        </div>

        <div class="flex items-center justify-start sm:justify-center gap-1 sm:gap-1.5 overflow-x-auto py-0.5 max-w-full">
          <template v-for="(set, sIdx) in settledWinningMelds" :key="`meld-${sIdx}`">
            <div class="flex items-center gap-0.5 bg-[#EFE5D3] px-1 py-0.5 rounded-lg border border-[#C89B3C]/50 shrink-0">
              <span class="text-[9px] font-bold text-[#8B2516] mr-0.5">{{ set.length === 4 ? '杠' : '碰' }}</span>
              <template v-for="(tile, tIdx) in set" :key="`meld-t-${tIdx}`">
                <div class="relative h-[34px] sm:h-[40px] w-[23px] sm:w-[27px] rounded bg-gradient-to-b from-[#FFFDF8] via-[#FAF4E8] to-[#EFE2C8] border border-[#C89B3C]/90 shadow-2xs flex items-center justify-center p-0.5 shrink-0">
                  <img :src="getMahjongImagePath(tile)" :alt="tile.text" class="w-full h-full object-contain pointer-events-none drop-shadow-2xs" />
                </div>
              </template>
            </div>
          </template>

          <div
            v-if="settledWinningMelds.length > 0"
            class="w-px h-6 bg-[#D8C7B0] shrink-0 mx-0.5"
          ></div>

          <template v-for="(tile, tIdx) in settledWinningHand" :key="tile.id || `hand-${tIdx}`">
            <div class="relative h-[38px] sm:h-[46px] w-[26px] sm:w-[32px] shrink-0 rounded-md sm:rounded-lg bg-gradient-to-b from-[#FFFDF8] via-[#FAF4E8] to-[#EFE2C8] border border-[#C89B3C]/80 shadow-xs flex items-center justify-center p-0.5">
              <img :src="getMahjongImagePath(tile)" :alt="tile.text" class="w-full h-full object-contain drop-shadow-2xs pointer-events-none" />
            </div>
          </template>
        </div>
      </div>

      <!-- 3. Reward Ma Tiles -->
      <div class="flex flex-col gap-0.5 shrink-0 bg-[#FAF4E8]/95 rounded-lg sm:rounded-xl px-2 py-1 border border-[#D8C7B0] shadow-xs">
        <div class="flex items-center justify-between text-[11px] sm:text-xs leading-tight">
          <span class="flex items-center gap-1.5 font-bold font-serif-title text-[#78350F]">
            <span>🎯</span>
            <span>开码奖码结果</span>
            <span class="text-[10px] px-1.5 py-0.2 rounded-full bg-[#FEF7E0] text-[#B06000] border border-[#F9AB00]/50 font-mono font-bold">
              开 {{ winningBreakdown.maCount }} 码 · 命中 {{ winningBreakdown.hitMaCount }} 码
            </span>
          </span>
          <span class="text-[10px] sm:text-[11px] text-[#6B5744] font-mono">
            {{ winningBreakdown.isFourHongzhong ? '🌟 四张红中：所有开码全中满贯！' : '命中【一、五、九、东风、红中】每中1码+10分' }}
          </span>
        </div>

        <div class="flex items-center justify-center gap-1 sm:gap-2 flex-wrap py-0.5">
          <template v-for="(tile, idx) in (winningBreakdown.rewardMaTiles || [])" :key="tile.id || `ma-${idx}`">
            <div
              class="relative flex flex-col items-center justify-between rounded-lg p-0.5 transition-all shadow-xs select-none"
              :class="isYiWuJiuMaTile(tile, winningBreakdown.isFourHongzhong)
                ? 'bg-gradient-to-b from-[#FEF3C7] to-[#FDE68A] border-2 border-[#D97706] ring-1 ring-[#D97706]/50 shadow-xs scale-102'
                : 'bg-[#F5EFE6] border border-[#D8C7B0] opacity-70'"
            >
              <div
                v-if="isYiWuJiuMaTile(tile, winningBreakdown.isFourHongzhong)"
                class="bg-gradient-to-r from-[#DC2626] to-[#EA580C] text-white font-black text-[8px] sm:text-[9px] px-1 py-0.1 rounded-full shadow-xs border border-[#FDE68A] animate-pulse whitespace-nowrap mb-0.5"
              >
                🔥 +10
              </div>
              <div
                v-else
                class="text-[7px] sm:text-[8px] text-[#78695C] font-bold px-0.5 rounded bg-[#E5D7C2] border border-[#D8C7B0] whitespace-nowrap mb-0.5"
              >
                未中
              </div>

              <div class="h-[34px] sm:h-[40px] w-[24px] sm:w-[28px] rounded bg-gradient-to-b from-[#FFFDF8] via-[#FAF4E8] to-[#EFE2C8] border border-[#C89B3C] flex items-center justify-center p-0.5 shadow-2xs">
                <img
                  :src="getMahjongImagePath(tile)"
                  :alt="tile.text"
                  class="w-full h-full object-contain drop-shadow-2xs pointer-events-none"
                />
              </div>

              <span
                class="text-[9px] sm:text-[10px] font-bold mt-0.5 font-mono leading-none"
                :class="isYiWuJiuMaTile(tile, winningBreakdown.isFourHongzhong) ? 'text-[#92400E] font-black' : 'text-[#78695C]'"
              >
                {{ tile.text }}
              </span>
            </div>
          </template>
        </div>
      </div>

      <!-- 4. Formula & 4-Player Settlement Row -->
      <div class="grid grid-cols-1 md:grid-cols-2 gap-1.5 shrink-0">
        <div class="bg-[#FAF4E8]/95 rounded-lg px-2 py-1 border border-[#D8C7B0] shadow-xs flex flex-col justify-between gap-1">
          <div class="flex items-center gap-1 flex-wrap text-[11px] sm:text-xs font-mono text-[#5C4533] leading-tight">
            <span class="text-[#78350F] font-bold">📐 计分：</span>
            <span>
              底分 10 + 中码 {{ winningBreakdown.hitMaCount }}×10 =
              <strong class="text-[#8B2516] font-bold">{{ winningBreakdown.payoutPerOpponent }}分/家</strong>
            </span>
            <span
              v-if="winningBreakdown.myKongNet !== undefined && winningBreakdown.myKongNet !== 0"
              class="px-1.5 py-0.2 rounded text-[10px] font-bold border"
              :class="winningBreakdown.myKongNet > 0
                ? 'bg-[#FEF7E0] border-[#F9AB00]/70 text-[#B06000]'
                : 'bg-[#FCE8E6] border-[#EA4335]/60 text-[#C5221F]'"
            >
              本席杠分 {{ winningBreakdown.myKongNet > 0 ? `+${winningBreakdown.myKongNet}` : winningBreakdown.myKongNet }}
            </span>
          </div>
          <div class="flex items-center gap-1 flex-wrap">
            <span class="text-[10px] font-bold text-[#78695C]">明细:</span>
            <span
              v-for="(fan, fIdx) in winningBreakdown.fans"
              :key="`fan-${fIdx}`"
              class="px-1.5 py-0.2 rounded bg-[#EFE5D3] text-[#78350F] border border-[#C89B3C]/40 text-[10px] sm:text-[11px] font-medium leading-none"
            >
              {{ fan }}
            </span>
          </div>
        </div>

        <div class="bg-[#FAF4E8]/95 rounded-lg px-2 py-1 border border-[#D8C7B0] shadow-xs flex flex-col justify-between gap-1">
          <div class="text-[11px] sm:text-xs font-bold text-[#78350F] font-serif-title flex items-center justify-between leading-tight">
            <span>四席筹码结算收支</span>
            <span class="text-[10px] text-[#78695C] font-mono font-normal">实时核算</span>
          </div>
          <div class="grid grid-cols-4 gap-1 text-center">
            <div
              class="rounded p-0.5 sm:p-1 border"
              :class="winningBreakdown.rewardCoins >= 0
                ? 'border-[#34A853]/60 bg-[#E6F4EA]/60 text-[#137333]'
                : 'border-[#EA4335]/40 bg-[#FCE8E6]/60 text-[#C5221F]'"
            >
              <div class="text-[9px] sm:text-[10px] text-[#5C4533] font-medium leading-none">本席</div>
              <div
                class="text-[10px] sm:text-xs font-bold font-mono tabular-nums mt-0.5 leading-none"
                :class="winningBreakdown.rewardCoins >= 0 ? 'text-[#137333]' : 'text-[#C5221F]'"
              >
                {{ winningBreakdown.rewardCoins >= 0 ? `+${winningBreakdown.rewardCoins}` : winningBreakdown.rewardCoins }}
              </div>
            </div>

            <template v-for="(oppSeat, oIdx) in [1, 2, 3]" :key="`seat-${oppSeat}`">
              <div
                class="rounded p-0.5 sm:p-1 border"
                :class="(winningBreakdown.netCoinsPerSeat ? winningBreakdown.netCoinsPerSeat[oppSeat] >= 0 : (winningBreakdown.winnerSeat === oppSeat))
                  ? 'border-[#34A853]/60 bg-[#E6F4EA]/60 text-[#137333]'
                  : 'border-[#D8C7B0] bg-[#FFFDF8] text-[#5C4533]'"
              >
                <div class="text-[9px] sm:text-[10px] text-[#6B5744] font-medium truncate leading-none">
                  {{ ['东席', '北席', '西席'][oIdx] }}
                </div>
                <div
                  class="text-[10px] sm:text-xs font-bold font-mono tabular-nums mt-0.5 leading-none"
                  :class="(winningBreakdown.netCoinsPerSeat ? winningBreakdown.netCoinsPerSeat[oppSeat] >= 0 : (winningBreakdown.winnerSeat === oppSeat))
                    ? 'text-[#137333]'
                    : 'text-[#5C4533]'"
                >
                  <template v-if="winningBreakdown.netCoinsPerSeat">
                    {{ winningBreakdown.netCoinsPerSeat[oppSeat] >= 0 ? `+${winningBreakdown.netCoinsPerSeat[oppSeat]}` : winningBreakdown.netCoinsPerSeat[oppSeat] }}
                  </template>
                  <template v-else>
                    {{ winningBreakdown.winnerSeat === oppSeat ? `+${winningBreakdown.payoutPerOpponent * 3}` : `-${winningBreakdown.payoutPerOpponent}` }}
                  </template>
                </div>
              </div>
            </template>
          </div>
        </div>
      </div>

      <!-- 5. Action Buttons -->
      <div class="flex items-center justify-between gap-2 pt-1 border-t border-[#D8C7B0] shrink-0">
        <button
          type="button"
          @click="emit('leave')"
          class="px-3.5 sm:px-4 py-1.5 rounded-lg bg-[#E5D7C2] hover:bg-[#D8C7B0] text-[#3C2C1E] border border-[#C89B3C]/60 text-xs sm:text-sm font-bold shadow-xs transition-all active:scale-95 cursor-pointer flex items-center gap-1"
        >
          <span>🚪</span>
          <span>退出大厅</span>
        </button>

        <div class="flex items-center gap-2">
          <span
            v-if="!hasClickedReady && isOnlineMultiplayerRoom"
            class="text-[11px] sm:text-xs font-bold text-[#78350F] bg-[#FAF4E8] px-2 py-1 rounded-md border border-[#D8C7B0]"
          >
            已有 {{ readyPlayersCount }}/4 人继续
          </span>
          <button
            type="button"
            @click="emit('restart')"
            :disabled="hasClickedReady"
            class="px-5 sm:px-8 py-1.5 sm:py-2 text-xs sm:text-sm md:text-base font-black rounded-lg shadow-md ring-1 transition-all active:scale-95 cursor-pointer flex items-center gap-1.5"
            :class="hasClickedReady
              ? 'bg-[#137333] text-white ring-emerald-300 opacity-90'
              : 'bg-gradient-to-r from-[#B45309] via-[#D97706] to-[#B45309] hover:from-[#92400E] hover:to-[#B45309] text-white shadow-amber-700/30 ring-amber-300'"
          >
            <span>{{ hasClickedReady ? `✅ 已继续 · 已有 ${readyPlayersCount}/4 人继续游戏...` : `继续下一局 🀄 (${settlementCountdown}s) ➔` }}</span>
          </button>
        </div>
      </div>
    </div>
  </div>

  <!-- Stalemate Overlay -->
  <div
    v-if="isDrawStalemate"
    class="absolute inset-0 z-40 flex items-center justify-center p-3 sm:p-4 select-none bg-black/75 backdrop-blur-md"
  >
    <div
      class="w-full max-w-md rounded-2xl border-2 border-[#C89B3C] shadow-2xl p-5 text-center flex flex-col items-center gap-3 text-[#3C2C1E]"
      :style="cardPaperStyle"
    >
      <div class="text-4xl filter drop-shadow-xs">🀄 🌪️</div>
      <div>
        <h3 class="text-lg sm:text-xl font-black text-[#8B2516] font-serif-title">
          荒牌流局
        </h3>
        <p class="text-xs sm:text-sm text-[#5C4533] mt-1 font-medium">
          {{ tipMessage || '牌墙已摸空，无人自摸，本局平手！' }}
        </p>
      </div>
      <div
        v-if="stalemateNetCoins.some((v) => v !== 0)"
        class="w-full bg-[#FAF4E8]/95 rounded-xl px-3 py-2 border border-[#D8C7B0] shadow-xs"
      >
        <div class="text-[11px] font-bold text-[#78350F] mb-1.5 flex items-center justify-between">
          <span>杠牌即时结算收支</span>
          <span class="text-[10px] font-mono font-normal text-[#78695C]">零和结算</span>
        </div>
        <div class="grid grid-cols-4 gap-1.5 text-center">
          <div
            v-for="(seatLabel, idx) in ['本席', '东席', '北席', '西席']"
            :key="`stale-${idx}`"
            class="rounded-lg p-1 border"
            :class="stalemateNetCoins[idx] > 0
              ? 'border-[#34A853]/60 bg-[#E6F4EA]/70 text-[#137333]'
              : (stalemateNetCoins[idx] < 0
                ? 'border-[#EA4335]/50 bg-[#FCE8E6]/70 text-[#C5221F]'
                : 'border-[#D8C7B0] bg-[#FFFDF8] text-[#5C4533]')"
          >
            <div class="text-[10px] font-medium">{{ seatLabel }}</div>
            <div class="text-xs font-bold font-mono tabular-nums mt-0.5">
              {{ stalemateNetCoins[idx] > 0 ? `+${stalemateNetCoins[idx]}` : stalemateNetCoins[idx] }}
            </div>
          </div>
        </div>
      </div>
      <div class="flex items-center gap-3 w-full mt-2 justify-center">
        <button
          type="button"
          @click="emit('leave')"
          class="px-4 py-2 bg-[#E5D7C2] hover:bg-[#D8C7B0] text-[#3C2C1E] text-xs sm:text-sm font-bold rounded-xl border border-[#C89B3C]/60 shadow-xs transition-all active:scale-95 cursor-pointer"
        >
          退出大厅 🚪
        </button>
        <button
          type="button"
          @click="emit('restart')"
          :disabled="hasClickedReady"
          class="px-6 py-2 text-xs sm:text-sm font-black rounded-xl shadow-md ring-1 active:scale-95 cursor-pointer text-white"
          :class="hasClickedReady ? 'bg-[#137333] ring-emerald-300 opacity-90' : 'bg-gradient-to-r from-[#B45309] via-[#D97706] to-[#B45309] hover:from-[#92400E] hover:to-[#B45309] ring-amber-300'"
        >
          {{ hasClickedReady ? `✅ 已继续 · 已有 ${readyPlayersCount}/4 人继续游戏...` : `继续下一局 🀄 (${settlementCountdown}s) ➔` }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { getMahjongImagePath } from '../../../utils/mahjongAssets';
import { isYiWuJiuMaTile, type MahjongTile, type WinningBreakdown } from '../../../games/mahjong/aiWinEngine';

defineProps<{
  hasWon: boolean;
  isDrawStalemate: boolean;
  winningBreakdown: WinningBreakdown;
  settledWinnerLabel: string;
  settledWinningMelds: MahjongTile[][];
  settledWinningHand: MahjongTile[];
  stalemateNetCoins: number[];
  tipMessage: string;
  hasClickedReady: boolean;
  isOnlineMultiplayerRoom: boolean;
  readyPlayersCount: number;
  settlementCountdown: number;
}>();

const emit = defineEmits<{
  (e: 'leave'): void;
  (e: 'restart'): void;
}>();

const cardPaperStyle = {
  backgroundColor: '#EFE5D3',
  backgroundImage: `radial-gradient(ellipse at 50% 30%, #FAF4E8 0%, #EFE5D3 65%, #E5D7C2 100%), url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='noiseFilter'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23noiseFilter)' opacity='0.035'/%3E%3C/svg%3E")`,
};
</script>
