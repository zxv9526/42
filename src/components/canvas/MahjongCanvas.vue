<template>
  <div
    ref="containerRef"
    class="w-full h-full select-none absolute inset-0 overflow-hidden flex items-center justify-center"
    :style="{
      backgroundColor: props.tableTheme?.feltColor || '#E8DCC0',
      background: `radial-gradient(ellipse at 50% 45%, ${props.tableTheme?.highlightColor || '#FAF4E8'} 0%, ${props.tableTheme?.feltColor || '#E8DCC0'} 60%, ${props.tableTheme?.edgeColor || '#D2C4A6'} 100%)`
    }"
  >
    <!-- Fullscreen Card Table Surface with In-Table Action Overlay -->
    <div class="w-full h-full absolute inset-0 overflow-hidden flex items-center justify-center">
      <canvas
        ref="canvasRef"
        @click="onCanvasClick"
        class="w-full h-full absolute inset-0 block cursor-pointer"
      ></canvas>

      <!-- Realistic 3D Mahjong Dice Cup Overlay (打色子定庄动效) -->
      <div
        v-if="dealingPhase === 'rolling_dice' && !hasWon && !isDrawStalemate"
        class="absolute inset-0 z-30 flex flex-col items-center justify-center p-3 select-none pointer-events-auto bg-black/65 backdrop-blur-xs"
      >
        <MahjongDiceCup
          :d1="dealerDice?.d1 || 1"
          :d2="dealerDice?.d2 || 1"
          :dealer-seat="dealerSeat"
          :is-dealer-continuing="isDealerContinuing"
          :title="isDealerContinuing ? `【${SEAT_NAMES[dealerSeat]}】连庄掷骰 · 确定起牌开门` : '打色子定庄 · 开门取牌'"
          :room-players="props.roomPlayers"
          @roll-finished="startDealingTilesAnimation"
          @skip="skipDealingAnimation"
        />
      </div>

      <!-- Dealing & Sorting Stage Overlay (抓牌理牌动效) -->
      <div
        v-else-if="dealingPhase !== 'playing' && !hasWon && !isDrawStalemate"
        class="absolute inset-0 pointer-events-none z-25 flex flex-col items-center justify-center select-none backdrop-blur-xs bg-black/55"
      >
        <div
          class="flex flex-col items-center justify-center p-4 sm:p-5 rounded-2xl sm:rounded-3xl bg-[#1C120C]/95 border-2 border-amber-400/80 backdrop-blur-md shadow-[0_16px_40px_rgba(0,0,0,0.9),0_0_30px_rgba(245,158,11,0.3)] pointer-events-auto transition-all transform scale-100 max-w-sm sm:max-w-md w-full mx-4"
        >
          <!-- Top Header Tag & Skip Button -->
          <div class="w-full flex items-center justify-between pb-2 mb-2 border-b border-amber-500/20 text-xs">
            <span class="text-amber-300 font-serif-title font-bold flex items-center gap-1.5">
              <span class="text-base animate-pulse">🀄</span>
              <span v-if="dealingPhase === 'dealing_tiles'">四道抓牌 · 牌背朝上发牌</span>
              <span v-else>整牌理牌 · 翻面亮牌</span>
            </span>
            <button
              type="button"
              @click="skipDealingAnimation"
              class="px-2.5 py-0.5 rounded-lg bg-amber-500/20 hover:bg-amber-500/35 text-amber-200 border border-amber-500/40 text-[11px] font-bold transition-all active:scale-95 cursor-pointer flex items-center gap-1 shadow-sm"
              title="跳过发牌动画，直接进入对局"
            >
              <span>跳过</span>
              <span>⏩</span>
            </button>
          </div>

          <div class="text-center w-full py-1">
            <div class="text-sm sm:text-base font-black text-transparent bg-clip-text bg-gradient-to-r from-amber-300 via-yellow-200 to-amber-400 font-serif-title drop-shadow mb-1">
              🎯 【{{ SEAT_NAMES[dealerSeat] }}】坐庄起手率先出牌
            </div>
            <div class="text-[11px] text-[#D4C3B3] font-mono">
              {{ dealingPhase === 'dealing_tiles' ? '四方茶客依序抓牌中...' : '整牌排理完毕，翻面开局！' }}
            </div>

            <!-- Dealing 4-Wave Progress Indicator -->
            <div class="mt-3 pt-2.5 border-t border-amber-500/20 flex items-center justify-center gap-1 sm:gap-1.5 text-[9px] sm:text-[10px] font-mono">
              <span
                class="px-2 py-0.5 rounded-lg border transition-all"
                :class="myHandDealtCount >= 4 ? 'bg-emerald-500/30 text-emerald-300 border-emerald-500/60 font-bold' : 'bg-white/5 text-white/35 border-white/10'"
              >1巡4张</span>
              <span class="text-white/30">→</span>
              <span
                class="px-2 py-0.5 rounded-lg border transition-all"
                :class="myHandDealtCount >= 8 ? 'bg-emerald-500/30 text-emerald-300 border-emerald-500/60 font-bold' : 'bg-white/5 text-white/35 border-white/10'"
              >2巡8张</span>
              <span class="text-white/30">→</span>
              <span
                class="px-2 py-0.5 rounded-lg border transition-all"
                :class="myHandDealtCount >= 12 ? 'bg-emerald-500/30 text-emerald-300 border-emerald-500/60 font-bold' : 'bg-white/5 text-white/35 border-white/10'"
              >3巡12张</span>
              <span class="text-white/30">→</span>
              <span
                class="px-2 py-0.5 rounded-lg border transition-all"
                :class="myHandDealtCount >= 13 ? 'bg-emerald-500/30 text-emerald-300 border-emerald-500/60 font-bold' : 'bg-white/5 text-white/35 border-white/10'"
              >4巡13张</span>
              <span class="text-white/30">→</span>
              <span
                class="px-2 py-0.5 rounded-lg border transition-all"
                :class="(dealerSeat === 0 ? myHandDealtCount >= 14 : oppHandsDealtCount[dealerSeat - 1] >= 14) ? 'bg-amber-500/35 text-amber-300 border-amber-500/60 font-black' : 'bg-white/5 text-white/35 border-white/10'"
              >跳牌14张</span>
            </div>
          </div>
        </div>
      </div>

      <!-- In-Table Integrated Action Buttons (Centered under player's hand for Peng/Gang/Hu/Pass claims) -->
      <div
        v-if="!hasWon && dealingPhase === 'playing' && (canPeng || canGang || canHu || isClaiming)"
        class="absolute bottom-4 sm:bottom-6 left-1/2 -translate-x-1/2 z-20 flex items-center gap-2 sm:gap-2.5 pointer-events-auto justify-center flex-wrap"
      >
        <!-- Peng (Allowed) with tile name -->
        <button
          v-if="canPeng"
          key="peng-btn"
          type="button"
          @click="handlePeng"
          class="px-4 sm:px-5 py-2 text-xs sm:text-sm font-extrabold rounded-xl bg-gradient-to-r from-emerald-600 to-teal-600 hover:from-emerald-500 hover:to-teal-500 text-white border-2 border-emerald-300 shadow-[0_4px_16px_rgba(16,185,129,0.5)] hover:scale-105 active:scale-95 transition-all animate-bounce cursor-pointer flex items-center gap-1.5"
        >
          <span>碰牌</span>
          <span v-if="lastDiscarded" class="text-amber-200 text-xs font-mono font-bold">【{{ lastDiscarded.text }}】</span>
        </button>

        <!-- Gang (Allowed) with tile name -->
        <button
          v-if="canGang"
          key="gang-btn"
          type="button"
          @click="handleGang"
          class="px-4 sm:px-5 py-2 text-xs sm:text-sm font-extrabold rounded-xl bg-gradient-to-r from-amber-600 to-orange-600 hover:from-amber-500 hover:to-orange-500 text-white border-2 border-amber-300 shadow-[0_4px_16px_rgba(245,158,11,0.5)] hover:scale-105 active:scale-95 transition-all animate-bounce cursor-pointer flex items-center gap-1.5"
        >
          <span>杠牌</span>
          <span v-if="lastDiscarded && turnPlayer !== 0" class="text-amber-200 text-xs font-mono font-bold">【{{ lastDiscarded.text }}】</span>
        </button>

        <!-- Pass Claim Button (过) -->
        <button
          v-if="isClaiming && (canPeng || canGang)"
          key="pass-btn"
          type="button"
          @click="handlePassClaim"
          class="px-3.5 sm:px-4 py-1.5 sm:py-2 text-xs sm:text-sm font-bold rounded-xl bg-gray-800/90 hover:bg-gray-700 text-gray-200 border border-white/20 shadow-lg hover:scale-105 active:scale-95 transition-all cursor-pointer flex items-center gap-1"
          title="放弃碰杠，继续对局"
        >
          <span>过</span>
          <span class="text-amber-300 font-mono font-bold text-xs tabular-nums">({{ claimCountdown }}s)</span>
        </button>

        <!-- Hu (Strictly Self-Draw 自摸 only, no Pao Hu) -->
        <button
          v-if="canHu"
          key="hu-btn"
          type="button"
          @click="handleHu"
          class="px-4 sm:px-6 py-1.5 text-xs sm:text-sm font-extrabold rounded-xl bg-gradient-to-r from-red-600 to-red-500 hover:from-red-500 hover:to-red-400 text-white shadow-[0_4px_20px_rgba(239,68,68,0.7)] hover:scale-110 active:scale-95 transition-all ring-2 ring-red-300 animate-pulse cursor-pointer"
        >
          自摸胡牌！🀄
        </button>
      </div>

      <!-- Long-standing Persistent Auto-Host Controls (Placed on Bottom-Left to prevent misclicking rightmost drawn tile) -->
      <div
        v-if="!hasWon && !isDrawStalemate && dealingPhase === 'playing'"
        class="absolute bottom-4 sm:bottom-6 z-30 pointer-events-auto transition-all"
        :class="isAutoHost ? 'left-1/2 -translate-x-1/2' : 'left-4 sm:left-6'"
      >
        <!-- Cancel Auto-Host Button -->
        <button
          v-if="isAutoHost"
          type="button"
          @click="cancelAutoHost"
          class="px-5 py-2 text-xs sm:text-sm font-extrabold rounded-full bg-gradient-to-r from-red-600 via-amber-600 to-orange-500 hover:from-red-500 hover:to-amber-400 text-white border-2 border-amber-300 shadow-[0_4px_20px_rgba(239,68,68,0.7)] active:scale-95 transition-all flex items-center gap-2 cursor-pointer"
        >
          <span>⚡ 自动托管中...</span>
          <span class="px-2 py-0.5 rounded-lg bg-black/40 text-amber-200 border border-white/20 font-bold hover:bg-black/70">取消托管</span>
        </button>

        <!-- Standalone Persistent Auto-Host Button (Left-4 sm:left-6) -->
        <button
          v-else
          type="button"
          @click="toggleAutoHost"
          class="px-3.5 sm:px-4 py-2 text-xs sm:text-sm font-bold rounded-2xl bg-[#241B14]/90 hover:bg-[#32251B] text-amber-300 border-2 border-amber-500/60 hover:border-amber-400 shadow-[0_4px_16px_rgba(0,0,0,0.6)] backdrop-blur-md transition-all active:scale-95 cursor-pointer flex items-center gap-1.5"
          title="开启智能托管：以麻将向听数、多门叫听与防守逻辑全自动代打"
        >
          <span class="text-amber-400">⚡</span>
          <span class="font-serif font-bold">托管</span>
        </button>
      </div>



      <MahjongSettlementModal
        :has-won="hasWon"
        :is-draw-stalemate="isDrawStalemate"
        :winning-breakdown="winningBreakdown"
        :settled-winner-label="settledWinnerLabel"
        :settled-winning-melds="settledWinningMelds"
        :settled-winning-hand="settledWinningHand"
        :stalemate-net-coins="stalemateNetCoins"
        :tip-message="tipMessage"
        :has-clicked-ready="hasClickedReady"
        :is-online-multiplayer-room="isOnlineMultiplayerRoom"
        :ready-players-count="readyPlayersCount"
        :settlement-countdown="settlementCountdown"
        @leave="handleLeaveToLobby"
        @restart="onRestartGame"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue';
import { soundFX } from '../../utils/sound';
import { haptics } from '../../utils/haptics';
import { TableTheme } from '../../utils/theme';
import { getCanvasEventCoords } from '../../utils/canvasCoords';
import {
  preloadMahjongAssets,
  onMahjongAssetsLoaded,
} from '../../utils/mahjongAssets';
import MahjongDiceCup from '../effects/MahjongDiceCup.vue';
import MahjongSettlementModal from './mahjong/MahjongSettlementModal.vue';
import { renderMahjongTableFrame } from './mahjong/mahjongRenderer';
import {
  type MahjongTile,
  type WinningBreakdown,
  isHongzhong,
  isYiWuJiuMaTile,
  sortMahjongHand as sortHand,
  createMahjongTilePool as createTilePool,
  normalizeServerMelds,
  checkMahjongWinLocal as checkMahjongWin,
  checkHongzhongWinLocal as checkHongzhongWin,
  shouldSmartPengHand,
  scoreMahjongHand,
  findSmartBestDiscardIdx,
} from '../../games/mahjong/aiWinEngine';

const props = defineProps<{
  tableTheme?: TableTheme;
  isRotated90?: boolean;
  sceneMode?: string;
  roomPlayers?: any[];
  currentUser?: any;
  roomId?: string;
  initialDeal?: any;
  reconnectSnapshot?: any;
  ws?: WebSocket | null;
  isPracticeMode?: boolean;
}>();

let lastAppliedReconnectStamp = '';

function getReconnectStamp(msg: any): string {
  if (!msg) return '';
  const rId = msg.room?.id || msg.roomId || '';
  return `${rId}:${msg.serverTs || 0}:${msg.boardState?.actionSeq ?? ''}:${msg.room?.status || ''}`;
}

function applyReconnectSnapshotIfMatching(snap: any) {
  if (!snap || snap.failed || props.isPracticeMode) return;
  const snapRoomId = snap.room?.id || snap.roomId;
  if (!snapRoomId || !props.roomId || snapRoomId !== props.roomId) return;
  const stamp = getReconnectStamp(snap);
  if (stamp && lastAppliedReconnectStamp === stamp) return;
  lastAppliedReconnectStamp = stamp;
  lastProcessedMoveCount = -1;
  lastProcessedMoveKey = '';
  handleReconnectSync(snap);
}

const autoManagedSeats = ref<Record<number, boolean>>({});
const disconnectedSeats = ref<Record<number, boolean>>({});

const mySeat = computed(() => {
  if (props.isPracticeMode || !props.currentUser) return 0;
  if (typeof props.currentUser?.seat === 'number' && props.currentUser.seat >= 0 && props.currentUser.seat <= 3) {
    return props.currentUser.seat;
  }
  if (!props.roomPlayers || props.roomPlayers.length === 0) return 0;
  const myId = props.currentUser?.id;
  const myName = props.currentUser?.name;

  if (myId) {
    const matched = props.roomPlayers.find((p: any) => p && (p.id === myId || p.originalId === myId));
    if (matched && typeof matched.seat === 'number') return matched.seat;
    const idx = props.roomPlayers.findIndex((p: any) => p && (p.id === myId || p.originalId === myId));
    if (idx !== -1) return idx;
  }

  if (myName) {
    const matched = props.roomPlayers.find((p: any) => p && (p.name === myName || (p as any).originalName === myName));
    if (matched && typeof matched.seat === 'number') return matched.seat;
    const idx = props.roomPlayers.findIndex((p: any) => p && p.name === myName);
    if (idx !== -1) return idx;
  }

  return 0;
});

const getSeatName = (seat: number) => {
  if (props.roomPlayers && props.roomPlayers.length > 0) {
    const absSeat = (mySeat.value + seat) % 4;
    const p = props.roomPlayers.find((x: any) => x.seat === absSeat) || props.roomPlayers[absSeat];
    if (p) {
      const prefix = seat === 0 ? '本家' : seat === 1 ? '东席' : seat === 2 ? '北席' : '西席';
      const isDisc = Boolean(p.isDisconnected || disconnectedSeats.value[absSeat]);
      const isAuto = Boolean(p.isAutoManaged || autoManagedSeats.value[absSeat]);
      const statusTag = isDisc ? ' [离线托管]' : isAuto ? ' [托管]' : '';
      return `${prefix} (${p.name})${statusTag}`;
    }
  }
  return ['您 (本席)', '东席 (牌友)', '北席 (牌友)', '西席 (牌友)'][seat] || '牌友席位';
};

const SEAT_NAMES = new Proxy([], {
  get(_target, prop) {
    const idx = Number(prop);
    if (!isNaN(idx)) {
      return getSeatName(idx);
    }
    return ['本家', '东席', '北席', '西席'][prop as any];
  }
}) as unknown as string[];

const emit = defineEmits<{
  (e: 'move', payload: any): void;
  (e: 'restart'): void;
  (e: 'fx', payload: any): void;
  (e: 'exit', force?: boolean): void;
  (e: 'game-over', payload?: any): void;
}>();

function isSeatAutoManaged(absSeat: number): boolean {
  if (absSeat === mySeat.value) return isAutoHost.value;
  const p = props.roomPlayers?.find((x: any) => x.seat === absSeat) || props.roomPlayers?.[absSeat];
  return Boolean(p?.isAutoManaged || p?.isDisconnected || autoManagedSeats.value[absSeat] || disconnectedSeats.value[absSeat]);
}

function isSeatDisconnected(absSeat: number): boolean {
  const p = props.roomPlayers?.find((x: any) => x.seat === absSeat) || props.roomPlayers?.[absSeat];
  return Boolean(p?.isDisconnected || disconnectedSeats.value[absSeat]);
}

const isOnlineMultiplayerRoom = computed(() => {
  return !props.isPracticeMode && Boolean(props.roomId) && !String(props.roomId).startsWith('practice-');
});

function isSeatBot(absSeat: number): boolean {
  if (absSeat === mySeat.value) return false;
  if (!props.isPracticeMode) return false;
  return true;
}

function relToAbsSeat(relSeat: number): number {
  return (mySeat.value + relSeat) % 4;
}

function absToRelSeat(absSeat: number): number {
  return (absSeat - mySeat.value + 4) % 4;
}

function sendGameMove(action: string, extraPayload: any = {}) {
  if (!props.isPracticeMode && props.ws && props.ws.readyState === WebSocket.OPEN && props.roomId) {
    props.ws.send(
      JSON.stringify({
        type: 'game_move',
        roomId: props.roomId,
        seat: mySeat.value,
        payload: {
          action,
          seat: mySeat.value,
          absSeat: mySeat.value,
          ...extraPayload,
        },
        event: {
          action,
          seat: mySeat.value,
          absSeat: mySeat.value,
          ...extraPayload,
        },
      })
    );
  }
}

watch(() => props.tableTheme, () => {
  drawTable();
});

// 仅当接收到全新权威发牌切片时才执行新开局，严禁因玩家列表属性变更重置当前进行中的牌桌手牌或结算界面
watch(() => props.initialDeal, (deal) => {
  if (props.isPracticeMode || !deal?.seatCards) return;
  const dealIdentifier = deal.dealId || deal.createdAt || (Array.isArray(deal.seatCards[0]) ? deal.seatCards[0].map((c: any) => c.id || c.name).join('-') : '');
  if (lastAppliedDealId.value && lastAppliedDealId.value === dealIdentifier) {
    if (hasWon.value || showSettlement.value || isDrawStalemate.value || dealingPhase.value === 'settled' || (myHand.value && myHand.value.length > 0)) {
      console.log('🛡️ [麻将防重置] 发牌标识一致，保持当前牌桌或结算界面');
      return;
    }
  }
  if ((hasWon.value || showSettlement.value || isDrawStalemate.value || dealingPhase.value === 'settled') && (!dealIdentifier || dealIdentifier === lastAppliedDealId.value)) {
    return;
  }
  dealNewMatch(undefined, false, deal);
}, { immediate: true });

// 玩家列表变化时（如掉线/托管更新），仅同步托管状态并仅在状态变化时重绘牌桌头像角标，绝不重置牌局！
let lastRoomPlayersSig = '';
watch(() => props.roomPlayers, (newPlayers) => {
  let sig = '';
  if (Array.isArray(newPlayers)) {
    sig = newPlayers
      .map((p: any, i: number) => `${typeof p?.seat === 'number' ? p.seat : i}:${p?.name || ''}:${p?.isAutoManaged ? 1 : 0}:${p?.isDisconnected ? 1 : 0}`)
      .join('|');
    newPlayers.forEach((p: any) => {
      if (typeof p?.seat === 'number') {
        if (p.isAutoManaged) autoManagedSeats.value[p.seat] = true;
        else if (p.isAutoManaged === false) delete autoManagedSeats.value[p.seat];
        if (p.isDisconnected) disconnectedSeats.value[p.seat] = true;
        else if (p.isDisconnected === false) delete disconnectedSeats.value[p.seat];
      }
    });
  }
  if (sig !== lastRoomPlayersSig) {
    lastRoomPlayersSig = sig;
    drawTable();
  }
}, { deep: true });

const containerRef = ref<HTMLDivElement | null>(null);
const tableWidthPx = ref(680);
const tableHeightPx = ref(430);
let resizeObserver: ResizeObserver | null = null;

function updateDimensions() {
  if (!containerRef.value) return;
  const availW = containerRef.value.clientWidth || 680;
  const availH = containerRef.value.clientHeight || 430;
  const targetAspect = 680 / 430;

  let w = availW;
  let h = w / targetAspect;

  if (h > availH) {
    h = availH;
    w = h * targetAspect;
  }

  tableWidthPx.value = Math.max(280, Math.floor(w));
  tableHeightPx.value = Math.max(180, Math.floor(h));
  drawTable();
}

const canvasRef = ref<HTMLCanvasElement | null>(null);
const myHand = ref<MahjongTile[]>([]);
const meldedTiles = ref<MahjongTile[][]>([]);
const discards = ref<MahjongTile[]>([]);
const wallCount = ref(83);
const selectedIndex = ref<number | null>(null);
const hasWon = ref(false);
const showSettlement = ref(false);
const announcingWinnerSeat = ref<number | null>(null);
let announceTimer: any = null;
let botActionTimers: any[] = [];
const tipMessage = ref('手牌理顺就绪，轮到您出牌');

// Opponents Hands (East: 0, North: 1, West: 2)
const oppHands = ref<MahjongTile[][]>([[], [], []]);
const oppMelds = ref<MahjongTile[][][]>([[], [], []]);
const oppCounts = computed(() => [
  oppHands.value[0]?.length || 0,
  oppHands.value[1]?.length || 0,
  oppHands.value[2]?.length || 0,
]);
const turnPlayer = ref<number>(0); // 0=South(player), 1=East, 2=North, 3=West
const lastDiscarded = ref<MahjongTile | null>(null);
const pendingPengTurn = ref<number | null>(null);

// 庄家与打色子定庄状态 (0=您/本席, 1=东席, 2=北席, 3=西席)
const dealerSeat = ref<number>(0);
const lastWinnerSeat = ref<number | null>(null); // 上局自摸胜者，继续游戏时由其继续坐庄率先出牌
const isDealerContinuing = ref<boolean>(false);
const dealerDice = ref<{ d1: number; d2: number; sum: number } | null>(null);
const dealingPhase = ref<'rolling_dice' | 'dealing_tiles' | 'sorting_tiles' | 'playing' | 'settled'>('rolling_dice');
const isRollingDice = ref(false);
const currentDiceDisplay = ref({ d1: 1, d2: 1 });
const myHandDealtCount = ref(0);
const oppHandsDealtCount = ref<number[]>([0, 0, 0]);
const handFlipProgress = ref(0); // 0 to 1
let diceRollInterval: any = null;
let dealingAnimTimers: any[] = [];
let flipRafId: any = null;

function getDicePipGrid(val: number): { visible: boolean; color: 'red' | 'black' }[] {
  const res: { visible: boolean; color: 'red' | 'black' }[] = Array.from({ length: 9 }, () => ({
    visible: false,
    color: 'black',
  }));

  const safeVal = Math.max(1, Math.min(6, val || 1));
  switch (safeVal) {
    case 1:
      res[4] = { visible: true, color: 'red' };
      break;
    case 2:
      res[0] = { visible: true, color: 'black' };
      res[8] = { visible: true, color: 'black' };
      break;
    case 3:
      res[0] = { visible: true, color: 'black' };
      res[4] = { visible: true, color: 'black' };
      res[8] = { visible: true, color: 'black' };
      break;
    case 4:
      res[0] = { visible: true, color: 'red' };
      res[2] = { visible: true, color: 'red' };
      res[6] = { visible: true, color: 'red' };
      res[8] = { visible: true, color: 'red' };
      break;
    case 5:
      res[0] = { visible: true, color: 'black' };
      res[2] = { visible: true, color: 'black' };
      res[4] = { visible: true, color: 'red' };
      res[6] = { visible: true, color: 'black' };
      res[8] = { visible: true, color: 'black' };
      break;
    case 6:
      res[0] = { visible: true, color: 'black' };
      res[3] = { visible: true, color: 'black' };
      res[6] = { visible: true, color: 'black' };
      res[2] = { visible: true, color: 'black' };
      res[5] = { visible: true, color: 'black' };
      res[8] = { visible: true, color: 'black' };
      break;
  }
  return res;
}

function cleanDealingTimers() {
  if (diceRollInterval) {
    clearInterval(diceRollInterval);
    diceRollInterval = null;
  }
  dealingAnimTimers.forEach((t) => clearTimeout(t));
  dealingAnimTimers = [];
  if (flipRafId) {
    cancelAnimationFrame(flipRafId);
    flipRafId = null;
  }
}

const isAllOtherPlayersBots = computed(() => {
  return Boolean(props.isPracticeMode);
});

const hasClickedReady = ref(false);
const readyPlayersCount = computed(() => {
  if (props.isPracticeMode) return 4;
  if (!props.roomPlayers || props.roomPlayers.length === 0) return hasClickedReady.value ? 1 : 0;
  const count = props.roomPlayers.filter((p: any, idx: number) => {
    const pSeat = typeof p?.seat === 'number' ? p.seat : idx;
    const isSelf =
      pSeat === mySeat.value ||
      (props.currentUser?.id && (p?.id === props.currentUser.id || p?.originalId === props.currentUser.id));
    if (isSelf) return hasClickedReady.value || Boolean(p?.isReady);
    return Boolean(p?.isReady);
  }).length;
  return Math.min(4, Math.max(0, count));
});

const isAutoHost = ref(false);
const isDrawStalemate = ref(false);
const turnCountdown = ref(60);
let countdownInterval: any = null;
let autoHostTimer: any = null;

const settlementCountdown = ref(60);
const settlementExpiresAtRef = ref<number | null>(null);
let settlementInterval: any = null;

function stopSettlementCountdown() {
  if (settlementInterval) {
    clearInterval(settlementInterval);
    settlementInterval = null;
  }
  settlementExpiresAtRef.value = null;
}

function startSettlementCountdown(expiresAt?: number | null, serverTs?: number | null) {
  stopSettlementCountdown();
  const now = Date.now();
  const offset = typeof serverTs === 'number' && serverTs > 0 ? now - serverTs : 0;
  const targetExpire = typeof expiresAt === 'number' && expiresAt > 0 ? expiresAt + offset : now + 60000;
  settlementExpiresAtRef.value = targetExpire;
  settlementCountdown.value = Math.max(0, Math.ceil((targetExpire - now) / 1000));

  settlementInterval = setInterval(() => {
    if (!hasWon.value && !isDrawStalemate.value) {
      stopSettlementCountdown();
      return;
    }
    const remMs = (settlementExpiresAtRef.value || Date.now()) - Date.now();
    settlementCountdown.value = Math.max(0, Math.ceil(remMs / 1000));
    if (remMs <= 0) {
      if (hasClickedReady.value) {
        // 已点击准备继续下一局，停在等待其他玩家或转入等待室，不退出大厅
        if (settlementInterval) {
          clearInterval(settlementInterval);
          settlementInterval = null;
        }
        return;
      }
      // 联机房间多留 1.5s 给服务端推送 settlement_timeout，超时兜底或试玩模式自动退出到大厅
      if (props.isPracticeMode || !props.ws || props.ws.readyState !== WebSocket.OPEN || remMs <= -1500) {
        stopSettlementCountdown();
        handleLeaveToLobby();
      }
    }
  }, 500);
}

function startTurnCountdown() {
  stopTurnCountdown();
  if (hasWon.value || isDrawStalemate.value) return;
  if (isAutoHost.value && turnPlayer.value === 0) {
    checkAutoHostTurn();
    return;
  }
  turnCountdown.value = 60;
  drawTable();
  countdownInterval = setInterval(() => {
    if (hasWon.value || isDrawStalemate.value) {
      stopTurnCountdown();
      return;
    }
    turnCountdown.value--;
    drawTable();
    if (turnCountdown.value <= 0) {
      stopTurnCountdown();
      if (turnPlayer.value === 0) {
        isAutoHost.value = true;
        sendGameMove('set_auto_host', { isAutoManaged: true });
        checkAutoHostTurn();
      } else {
        const absSeat = relToAbsSeat(turnPlayer.value);
        if (props.isPracticeMode && isSeatBot(absSeat)) {
          botPlay(turnPlayer.value, () => passTurnToNext(turnPlayer.value));
        }
      }
      drawTable();
    }
  }, 1000);
}

function stopTurnCountdown() {
  if (countdownInterval) {
    clearInterval(countdownInterval);
    countdownInterval = null;
    drawTable();
  }
}

function cancelAutoHost() {
  isAutoHost.value = false;
  sendGameMove('set_auto_host', { isAutoManaged: false });
  if (autoHostTimer) {
    clearTimeout(autoHostTimer);
    autoHostTimer = null;
  }
  soundFX.playTap();
  if (turnPlayer.value === 0 && !hasWon.value && !isDrawStalemate.value) {
    startTurnCountdown();
  }
  drawTable();
}

function toggleAutoHost() {
  isAutoHost.value = !isAutoHost.value;
  sendGameMove('set_auto_host', { isAutoManaged: isAutoHost.value });
  soundFX.playTap();
  if (isAutoHost.value) {
    stopTurnCountdown();
    if (turnPlayer.value === 0 && !hasWon.value && !isDrawStalemate.value) {
      checkAutoHostTurn();
    }
  } else {
    if (autoHostTimer) {
      clearTimeout(autoHostTimer);
      autoHostTimer = null;
    }
    if (turnPlayer.value === 0 && !hasWon.value && !isDrawStalemate.value) {
      startTurnCountdown();
    }
  }
  drawTable();
}

function resetAutoHostState() {
  isAutoHost.value = false;
  if (autoHostTimer) {
    clearTimeout(autoHostTimer);
    autoHostTimer = null;
  }
}

// Claim Window (碰 / 杠思考窗口，时长 10 秒，支持点击【过】提前跳过)
const isClaiming = ref(false);
const claimCountdown = ref(10);
const isWaitingServerClaimWindow = ref(false);
let isOnlineDrawPending = false;
let isOnlineGangPending = false;
let isOnlineHuPending = false;
let onlinePendingWatchdog: any = null;
let pendingOnlineGangBackup: { playerKongExtra: number[]; seatKongScores: number[]; kongRecords: string[] } | null = null;
let claimInterval: any = null;
let claimWaitTimer: any = null;
let pendingClaimNext: (() => void) | null = null;

function clearOnlinePendingFlags() {
  isOnlineDrawPending = false;
  isOnlineGangPending = false;
  isOnlineHuPending = false;
  if (onlinePendingWatchdog) {
    clearTimeout(onlinePendingWatchdog);
    onlinePendingWatchdog = null;
  }
}

function armOnlinePendingWatchdog() {
  if (onlinePendingWatchdog) {
    clearTimeout(onlinePendingWatchdog);
  }
  onlinePendingWatchdog = setTimeout(() => {
    onlinePendingWatchdog = null;
    if (isOnlineDrawPending || isOnlineGangPending || isOnlineHuPending) {
      isOnlineDrawPending = false;
      isOnlineGangPending = false;
      isOnlineHuPending = false;
      drawTable();
    }
  }, 6000);
}

function stopClaimCountdown() {
  isClaiming.value = false;
  pendingClaimNext = null;
  if (claimInterval) {
    clearInterval(claimInterval);
    claimInterval = null;
  }
  if (claimWaitTimer) {
    clearTimeout(claimWaitTimer);
    claimWaitTimer = null;
  }
}

function cleanAllGameTimers() {
  stopTurnCountdown();
  stopClaimCountdown();
  resetAutoHostState();
  cleanDealingTimers();
  pendingPengTurn.value = null;
  isWaitingServerClaimWindow.value = false;
  clearOnlinePendingFlags();
  pendingOnlineGangBackup = null;
  lastOptimisticDrawId = null;
  lastOptimisticSuppTileId = null;
  lastDrawnTileIdByRelSeat.fill(null);
  if (botTimer) {
    clearTimeout(botTimer);
    botTimer = null;
  }
  if (announceTimer) {
    clearTimeout(announceTimer);
    announceTimer = null;
  }
  botActionTimers.forEach((t) => clearTimeout(t));
  botActionTimers = [];
  pendingClaimNext = null;
}

function handlePassClaim() {
  soundFX.playTap();
  const dId = lastDiscarded.value?.id;
  sendGameMove('pass', { absSeat: mySeat.value, discardId: dId });
  const next = pendingClaimNext;
  stopClaimCountdown();
  if (isOnlineMultiplayerRoom.value && props.ws && props.ws.readyState === WebSocket.OPEN) {
    tipMessage.value = '🀄 已选择过牌，等待牌桌流转...';
    drawTable();
    return;
  }
  if (next) {
    next();
  }
}

function shouldSmartPeng(hand: MahjongTile[], tile: MahjongTile): boolean {
  return shouldSmartPengHand(hand, tile, isHongzhongMode.value);
}

function waitForPlayerClaim(next: () => void) {
  stopClaimCountdown();
  if (hasWon.value || showSettlement.value || isDrawStalemate.value || dealingPhase.value !== 'playing') return;

  // 托管状态下的智能截击判定（托管自动明杠或智能碰牌）
  if (isAutoHost.value && !hasWon.value && !isDrawStalemate.value && dealingPhase.value === 'playing') {
    if (canGang.value) {
      claimWaitTimer = setTimeout(() => {
        claimWaitTimer = null;
        if (hasWon.value || showSettlement.value || dealingPhase.value !== 'playing') return;
        handleGang();
      }, 500);
      return;
    }
    if (canPeng.value) {
      const d = lastDiscarded.value;
      if (d && shouldSmartPeng(myHand.value, d)) {
        claimWaitTimer = setTimeout(() => {
          claimWaitTimer = null;
          if (hasWon.value || showSettlement.value || dealingPhase.value !== 'playing') return;
          handlePeng();
        }, 500);
        return;
      }
    }
    claimWaitTimer = setTimeout(() => {
      claimWaitTimer = null;
      if (hasWon.value || showSettlement.value || dealingPhase.value !== 'playing' || !lastDiscarded.value || pendingPengTurn.value !== null) return;
      if (isOnlineMultiplayerRoom.value && props.ws && props.ws.readyState === WebSocket.OPEN) {
        sendGameMove('pass', { absSeat: mySeat.value, discardId: lastDiscarded.value?.id });
        return;
      }
      next();
    }, 550);
    return;
  }

  // 检查玩家是否有碰或杠的机会 (真人操作)
  if ((canPeng.value || canGang.value) && !hasWon.value && !isDrawStalemate.value && dealingPhase.value === 'playing') {
    isClaiming.value = true;
    claimCountdown.value = 10;
    pendingClaimNext = next;

    claimInterval = setInterval(() => {
      if (hasWon.value || showSettlement.value || dealingPhase.value !== 'playing') {
        stopClaimCountdown();
        return;
      }
      claimCountdown.value--;
      if (claimCountdown.value <= 0) {
        // 10秒超时自动过
        if (isOnlineMultiplayerRoom.value && props.ws && props.ws.readyState === WebSocket.OPEN) {
          handlePassClaim();
          return;
        }
        const cb = pendingClaimNext;
        stopClaimCountdown();
        if (cb && !hasWon.value && !showSettlement.value && dealingPhase.value === 'playing') cb();
      }
    }, 1000);
  } else {
    if (isOnlineMultiplayerRoom.value && props.ws && props.ws.readyState === WebSocket.OPEN) {
      return;
    }
    const waitDelay = 550;
    claimWaitTimer = setTimeout(() => {
      claimWaitTimer = null;
      if (hasWon.value || showSettlement.value || dealingPhase.value !== 'playing' || !lastDiscarded.value || pendingPengTurn.value !== null) return;
      next();
    }, waitDelay);
  }
}

function scoreHand(tiles: MahjongTile[], evaluateTing: boolean = false): number {
  return scoreMahjongHand(
    tiles,
    {
      hongzhongMode: isHongzhongMode.value,
      discards: discards.value,
      meldedTiles: meldedTiles.value,
    },
    evaluateTing
  );
}

function findSmartBestDiscard(hand: MahjongTile[]): number {
  return findSmartBestDiscardIdx(hand, {
    hongzhongMode: isHongzhongMode.value,
    discards: discards.value,
    meldedTiles: meldedTiles.value,
    oppMelds: oppMelds.value,
    wallCount: wallCount.value,
  });
}

function checkAutoHostTurn() {
  stopTurnCountdown();
  if (!isAutoHost.value || turnPlayer.value !== 0 || hasWon.value || isDrawStalemate.value) return;
  if (isOnlineDrawPending || isOnlineGangPending || isOnlineHuPending || isWaitingServerClaimWindow.value) return;
  if (autoHostTimer) clearTimeout(autoHostTimer);
  autoHostTimer = setTimeout(() => {
    if (!isAutoHost.value || turnPlayer.value !== 0 || hasWon.value || isDrawStalemate.value) return;
    if (isOnlineDrawPending || isOnlineGangPending || isOnlineHuPending || isWaitingServerClaimWindow.value) return;
    if (canHu.value) {
      handleHu();
      return;
    }
    if (canGang.value) {
      handleGang();
      return;
    }
    if (myHand.value.length % 3 === 2) {
      const bestDiscardIdx = findSmartBestDiscard(myHand.value);
      const safeIdx = Math.max(0, Math.min(myHand.value.length - 1, bestDiscardIdx));
      const discarded = myHand.value.splice(safeIdx, 1)[0];
      if (discarded) {
        discarded.discardBy = 0;
        myHand.value = sortHand(myHand.value);
        discards.value.push(discarded);
        lastDiscarded.value = discarded;
        soundFX.playMahjongClack();
        soundFX.speakVoice(discarded.text);
        selectedIndex.value = null;
        tipMessage.value = `⚡ 托管打出了【${discarded.text}】到牌池`;
        drawTable();
        emit('move', { action: 'discard', tile: discarded });
        sendGameMove('discard', { tile: discarded, absSeat: mySeat.value });
        passTurnToNext(0);
      }
    } else if (myHand.value.length % 3 === 1) {
      handleDraw();
    }
  }, 650);
}





const playerKongExtra = ref<number[]>([0, 0, 0]); // [East, North, West] extra points owed by opponents due to player's Kongs
// 全席位杠分净盈亏记账本 [0:玩家, 1:东席, 2:北席, 3:西席]
const seatKongScores = ref<number[]>([0, 0, 0, 0]);
const stalemateNetCoins = ref<number[]>([0, 0, 0, 0]);
const kongRecords = ref<string[]>([]);

function formatKongHistoryRecords(history: any[]): string[] {
  if (!Array.isArray(history)) return [];
  return history.map((entry: any) => {
    const relGanger = typeof entry?.seat === 'number' ? absToRelSeat(entry.seat) : -1;
    const gangerName = relGanger === 0 ? '您' : `【${SEAT_NAMES[relGanger] || '牌友'}】`;
    if (entry?.gangType === 'ming' && typeof entry.fromSeat === 'number') {
      const relVictim = absToRelSeat(entry.fromSeat);
      const victimName = relVictim === 0 ? '您' : `【${SEAT_NAMES[relVictim] || '牌友'}】`;
      if (relGanger === 0) return `您明杠${victimName} (+10分)`;
      if (relVictim === 0) return `${gangerName}明杠您 (-10分)`;
      return `${gangerName}明杠${victimName} (10分)`;
    }
    const label = entry?.gangType === 'an' ? '暗杠' : '补杠';
    if (relGanger === 0) return `您${label}各得三家10分 (+30分)`;
    return `${gangerName}${label}各家输10分 (-10分)`;
  });
}

const winningBreakdown = ref<WinningBreakdown>({
  fans: ['自摸和牌 (基准 1番)', '常规平胡 (不计翻倍)'],
  totalFan: 1,
  rewardCoins: 100,
  maCount: 6,
  rewardMaTiles: [],
  hitMaCount: 0,
  payoutPerOpponent: 10,
  isFourHongzhong: false,
  netCoinsPerSeat: [0, 0, 0, 0],
  winningHand: [],
  winningMelds: [],
  winnerNameSnapshot: '',
});

const settledWinnerLabel = computed(() => {
  const wSeat = winningBreakdown.value.winnerSeat ?? (lastWinnerSeat.value ?? dealerSeat.value);
  if (props.roomPlayers && props.roomPlayers.length > 0) {
    return getSeatName(wSeat);
  }
  return winningBreakdown.value.winnerNameSnapshot || SEAT_NAMES[wSeat] || '牌友';
});

const settledWinningMelds = computed<MahjongTile[][]>(() => {
  if (Array.isArray(winningBreakdown.value.winningMelds) && winningBreakdown.value.winningMelds.length > 0) {
    return winningBreakdown.value.winningMelds;
  }
  const wSeat = winningBreakdown.value.winnerSeat ?? 0;
  return wSeat === 0 ? meldedTiles.value : (oppMelds.value[wSeat - 1] || []);
});

const settledWinningHand = computed<MahjongTile[]>(() => {
  if (Array.isArray(winningBreakdown.value.winningHand) && winningBreakdown.value.winningHand.length > 0) {
    return sortHand(winningBreakdown.value.winningHand);
  }
  const wSeat = winningBreakdown.value.winnerSeat ?? 0;
  const raw = wSeat === 0 ? myHand.value : (oppHands.value[wSeat - 1] || []);
  return sortHand(raw);
});

const isHongzhongMode = computed(() => {
  return props.sceneMode === 'hongzhong-6' || props.sceneMode === 'hongzhong-10';
});

const baseMaCount = computed(() => {
  return (props.sceneMode === 'standard-10' || props.sceneMode === 'hongzhong-10') ? 10 : 6;
});

function isYiWuJiuMa(t: MahjongTile): boolean {
  return isYiWuJiuMaTile(t, Boolean(winningBreakdown.value?.isFourHongzhong));
}

let wallPool: MahjongTile[] = [];

function handleLeaveToLobby() {
  stopSettlementCountdown();
  clearLocalSnapshot();
  try {
    localStorage.removeItem('app_active_game_lock');
  } catch {}
  if (props.ws && props.ws.readyState === WebSocket.OPEN && !props.isPracticeMode) {
    try {
      props.ws.send(
        JSON.stringify({
          type: 'scene_leave',
          roomId: props.roomId,
          playerId: props.currentUser?.id,
          playerName: props.currentUser?.name,
          gameType: 'mahjong',
          sceneMode: props.sceneMode,
        })
      );
    } catch {}
  }
  emit('exit', true);
}

function onRestartGame() {
  soundFX.playTap();
  cleanAllGameTimers();
  if (props.isPracticeMode) {
    stopSettlementCountdown();
    showSettlement.value = false;
    hasWon.value = false;
    isDrawStalemate.value = false;
    hasClickedReady.value = false;
    clearLocalSnapshot();
    continueGame();
    emit('restart');
    return;
  }

  hasClickedReady.value = true;
  saveLocalSnapshotNow();
  if (props.ws && props.ws.readyState === WebSocket.OPEN) {
    try {
      props.ws.send(JSON.stringify({
        type: 'scene_player_ready',
        roomId: props.roomId,
        playerId: props.currentUser?.id,
        playerName: props.currentUser?.name,
        gameType: 'mahjong',
        sceneMode: props.sceneMode,
        isReady: true,
      }));
    } catch {}
  }
}

function continueGame() {
  resetAutoHostState();
  // 谁自摸谁坐庄，率先开牌出牌
  const nextDealer = lastWinnerSeat.value !== null ? lastWinnerSeat.value : dealerSeat.value;
  dealNewMatch(nextDealer);
}

const lastAppliedDealId = ref<string>('');
let lastOptimisticDrawId: string | null = null;
let lastOptimisticSuppTileId: string | null = null;
const lastDrawnTileIdByRelSeat: (string | null)[] = [null, null, null, null];
let snapshotTimer: any = null;

function clearLocalSnapshot() {
  if (snapshotTimer) {
    clearTimeout(snapshotTimer);
    snapshotTimer = null;
  }
  const roomId = props.roomId;
  if (!roomId) return;
  try {
    localStorage.removeItem(`mahjong_table_snapshot_${roomId}`);
    localStorage.removeItem('mahjong_table_snapshot_latest');
  } catch {}
}

function saveLocalSnapshotNow() {
  if (props.isPracticeMode) return;
  const roomId = props.roomId;
  if (!roomId) return;
  const isSettledState = hasWon.value || isDrawStalemate.value || showSettlement.value || dealingPhase.value === 'settled';
  if (!isSettledState && (dealingPhase.value !== 'playing' || !myHand.value || myHand.value.length === 0)) return;

  try {
    const snap = {
      dealId: lastAppliedDealId.value,
      roomId,
      mySeat: mySeat.value,
      myHand: myHand.value,
      oppCounts: oppCounts.value,
      oppHands: oppHands.value,
      discards: discards.value,
      lastDiscarded: lastDiscarded.value,
      meldedTiles: meldedTiles.value,
      oppMelds: oppMelds.value,
      dealerSeat: dealerSeat.value,
      dealerDice: dealerDice.value,
      turnPlayer: turnPlayer.value,
      wallCount: wallCount.value,
      wallPool: wallPool,
      seatKongScores: seatKongScores.value,
      stalemateNetCoins: stalemateNetCoins.value,
      playerKongExtra: playerKongExtra.value,
      kongRecords: kongRecords.value,
      isHongzhongMode: isHongzhongMode.value,
      isSettled: isSettledState,
      hasWon: hasWon.value,
      showSettlement: showSettlement.value,
      isDrawStalemate: isDrawStalemate.value,
      winningBreakdown: winningBreakdown.value,
      lastWinnerSeat: lastWinnerSeat.value,
      hasClickedReady: hasClickedReady.value,
      settlementExpiresAt: settlementExpiresAtRef.value,
      tipMessage: tipMessage.value,
      timestamp: Date.now(),
    };
    const serialized = JSON.stringify(snap);
    localStorage.setItem(`mahjong_table_snapshot_${roomId}`, serialized);
  } catch {}
}

function saveLocalSnapshot() {
  if (props.isPracticeMode) return;
  if (snapshotTimer) clearTimeout(snapshotTimer);
  snapshotTimer = setTimeout(() => {
    snapshotTimer = null;
    saveLocalSnapshotNow();
  }, 300);
}

function tryRestoreLocalSnapshot(): boolean {
  if (props.isPracticeMode) return false;
  const roomId = props.roomId;
  if (!roomId) return false;
  try {
    const raw = localStorage.getItem(`mahjong_table_snapshot_${roomId}`);
    if (!raw) return false;
    const snap = JSON.parse(raw);
    if (!snap || snap.roomId !== roomId) return false;
    if (!snap.isSettled && (!Array.isArray(snap.myHand) || snap.myHand.length === 0)) return false;
    if (typeof snap.mySeat === 'number' && snap.mySeat !== mySeat.value) return false;
    if (Date.now() - (snap.timestamp || 0) > 2 * 3600 * 1000) {
      clearLocalSnapshot();
      return false;
    }
    const currentDeal = props.initialDeal;
    const dealIdentifier = currentDeal?.dealId || currentDeal?.createdAt || (Array.isArray(currentDeal?.seatCards?.[0]) ? currentDeal.seatCards[0].map((c: any) => c.id || c.name).join('-') : '');
    if (dealIdentifier && snap.dealId && snap.dealId !== dealIdentifier) {
      return false;
    }

    lastAppliedDealId.value = snap.dealId || dealIdentifier || '';
    myHand.value = Array.isArray(snap.myHand) ? sortHand(snap.myHand) : [];
    if (Array.isArray(snap.oppHands)) oppHands.value = snap.oppHands;
    discards.value = Array.isArray(snap.discards) ? snap.discards : [];
    lastDiscarded.value = snap.lastDiscarded || null;
    meldedTiles.value = Array.isArray(snap.meldedTiles) ? snap.meldedTiles : [];
    oppMelds.value = Array.isArray(snap.oppMelds) ? snap.oppMelds : [[], [], []];
    dealerSeat.value = snap.dealerSeat ?? 0;
    if (snap.dealerDice) {
      dealerDice.value = snap.dealerDice;
      currentDiceDisplay.value = { d1: snap.dealerDice.d1, d2: snap.dealerDice.d2 };
    }
    turnPlayer.value = snap.turnPlayer ?? 0;
    wallCount.value = snap.wallCount ?? 83;
    if (Array.isArray(snap.wallPool)) wallPool = snap.wallPool;
    if (Array.isArray(snap.seatKongScores)) seatKongScores.value = snap.seatKongScores;
    if (Array.isArray(snap.stalemateNetCoins)) stalemateNetCoins.value = snap.stalemateNetCoins;
    if (Array.isArray(snap.playerKongExtra)) playerKongExtra.value = snap.playerKongExtra;
    if (Array.isArray(snap.kongRecords)) kongRecords.value = snap.kongRecords;

    cleanDealingTimers();
    isRollingDice.value = false;
    myHandDealtCount.value = myHand.value.length;
    oppHandsDealtCount.value = [oppCounts.value[0], oppCounts.value[1], oppCounts.value[2]];
    handFlipProgress.value = 1;

    if (snap.isSettled) {
      dealingPhase.value = 'settled';
      hasWon.value = Boolean(snap.hasWon);
      showSettlement.value = Boolean(snap.showSettlement);
      isDrawStalemate.value = Boolean(snap.isDrawStalemate);
      lastWinnerSeat.value = snap.lastWinnerSeat ?? null;
      hasClickedReady.value = Boolean(snap.hasClickedReady);
      if (snap.winningBreakdown) {
        winningBreakdown.value = {
          ...snap.winningBreakdown,
          winningHand: Array.isArray(snap.winningBreakdown.winningHand) ? sortHand(snap.winningBreakdown.winningHand) : [],
          winningMelds: normalizeServerMelds(snap.winningBreakdown.winningMelds || []),
        };
      }
      if (snap.tipMessage) tipMessage.value = snap.tipMessage;
      startSettlementCountdown(snap.settlementExpiresAt, Date.now());
      drawTable();
      console.log('🀄 [麻将结算断线重连] 瞬时恢复结算界面状态');
      return true;
    }

    dealingPhase.value = 'playing';
    tipMessage.value = turnPlayer.value === 0 ? '🔄 牌桌状态已恢复！轮到您出牌' : `🔄 牌桌状态已恢复！当前轮到【${SEAT_NAMES[turnPlayer.value]}】出牌`;
    if (turnPlayer.value === 0 && !isClaiming.value) {
      startTurnCountdown();
    }
    drawTable();
    console.log(`🀄 [麻将断线重连] 瞬时恢复牌桌状态：手牌 ${myHand.value.length} 张，弃牌池 ${discards.value.length} 张`);
    return true;
  } catch (err) {
    console.warn('[Mahjong Snapshot Restore Error]', err);
  }
  return false;
}

function handleReconnectSync(msg: any) {
  if (!msg) return;
  stopTurnCountdown();
  stopClaimCountdown();
  cleanDealingTimers();
  isRollingDice.value = false;
  dealingPhase.value = 'playing';
  handFlipProgress.value = 1;
  isWaitingServerClaimWindow.value = false;
  clearOnlinePendingFlags();
  pendingOnlineGangBackup = null;
  lastOptimisticDrawId = null;
  lastOptimisticSuppTileId = null;
  lastDrawnTileIdByRelSeat.fill(null);

  const bs = msg.boardState;
  const deal = msg.deal;
  const isServerEnded = bs?.phase === 'ended' || msg.room?.status === 'finished';

  if (Array.isArray(msg.players)) {
    msg.players.forEach((p: any) => {
      if (p && typeof p.seat === 'number') {
        if (p.isAutoManaged) autoManagedSeats.value[p.seat] = true;
        else delete autoManagedSeats.value[p.seat];
        if (p.isDisconnected) disconnectedSeats.value[p.seat] = true;
        else delete disconnectedSeats.value[p.seat];
      }
    });
    const selfPl = msg.players.find(
      (p: any) =>
        p &&
        (p.seat === mySeat.value ||
          (props.currentUser?.id && (p.id === props.currentUser.id || p.originalId === props.currentUser.id)))
    );
    if (selfPl) {
      hasClickedReady.value = Boolean(selfPl.isReady);
    }
  }

  if (isServerEnded) {
    dealingPhase.value = 'settled';
  } else if (bs?.phase && bs.phase !== 'ended') {
    stopSettlementCountdown();
    hasWon.value = false;
    showSettlement.value = false;
    isDrawStalemate.value = false;
    hasClickedReady.value = false;
  }
  if (bs?.autoManagedSeats) {
    for (let s = 0; s < 4; s++) {
      if (bs.autoManagedSeats[s]) autoManagedSeats.value[s] = true;
      else delete autoManagedSeats.value[s];
    }
    isAutoHost.value = Boolean(bs.autoManagedSeats[mySeat.value]);
  }

  if (deal) {
    const dealId = deal.dealId || deal.createdAt || '';
    if (dealId) lastAppliedDealId.value = dealId;
    if (deal.dice) {
      dealerDice.value = deal.dice;
      currentDiceDisplay.value = { d1: deal.dice.d1, d2: deal.dice.d2 };
    }
    if (deal.dealerSeat !== undefined) {
      dealerSeat.value = (deal.dealerSeat - mySeat.value + 4) % 4;
    }
    if (Array.isArray(deal.remainingWall)) {
      wallPool = [...deal.remainingWall];
      wallCount.value = wallPool.length;
    }
  }

  // Restore river discards from server authoritative board state
  if (bs?.extraState?.mahjongDiscards && Array.isArray(bs.extraState.mahjongDiscards)) {
    discards.value = bs.extraState.mahjongDiscards.map((d: any) => {
      const absS = typeof d.discardBy === 'number' ? d.discardBy : (typeof d.absSeat === 'number' ? d.absSeat : 0);
      return {
        ...d,
        discardBy: absToRelSeat(absS),
      };
    });
    if (bs.extraState.lastDiscarded) {
      const lAbs = typeof bs.extraState.lastDiscarded.discardBy === 'number' ? bs.extraState.lastDiscarded.discardBy : 0;
      lastDiscarded.value = {
        ...bs.extraState.lastDiscarded,
        discardBy: absToRelSeat(lAbs),
      };
    } else {
      lastDiscarded.value = null;
    }
  }

  // Restore melds from server (normalize server meld objects to MahjongTile[][])
  if (bs?.extraState?.mahjongMelds && Array.isArray(bs.extraState.mahjongMelds)) {
    const sMe = mySeat.value;
    meldedTiles.value = normalizeServerMelds(bs.extraState.mahjongMelds[sMe] || []);
    for (let r = 1; r < 4; r++) {
      const absR = relToAbsSeat(r);
      oppMelds.value[r - 1] = normalizeServerMelds(bs.extraState.mahjongMelds[absR] || []);
    }
  }

  if (Array.isArray(bs?.seatKongScores) && bs.seatKongScores.length === 4) {
    const mappedKong = [0, 0, 0, 0];
    for (let relS = 0; relS < 4; relS++) {
      mappedKong[relS] = bs.seatKongScores[relToAbsSeat(relS)] || 0;
    }
    seatKongScores.value = mappedKong;
  }
  const reconKongHist = bs?.extraState?.kongHistory || bs?.settlement?.kongHistory;
  if (Array.isArray(reconKongHist)) {
    kongRecords.value = formatKongHistoryRecords(reconKongHist);
  }

  if (bs?.extraState?.pendingPengSeat !== undefined) {
    pendingPengTurn.value = bs.extraState.pendingPengSeat !== null
      ? absToRelSeat(bs.extraState.pendingPengSeat)
      : null;
  }

  if (bs?.turnSeat !== undefined) {
    turnPlayer.value = absToRelSeat(bs.turnSeat);
  }

  if (bs?.extraState?.wallRemaining !== undefined) {
    wallCount.value = bs.extraState.wallRemaining;
  }

  if (deal?.seatCards) {
    myHand.value = sortHand([...(deal.seatCards[mySeat.value] || [])]);
    oppHands.value = [
      sortHand([...(deal.seatCards[relToAbsSeat(1)] || [])]),
      sortHand([...(deal.seatCards[relToAbsSeat(2)] || [])]),
      sortHand([...(deal.seatCards[relToAbsSeat(3)] || [])]),
    ];
  }
  if (!isServerEnded && Array.isArray(bs?.handsCount) && bs.handsCount.length === 4) {
    for (let r = 1; r < 4; r++) {
      const targetLen = Number(bs.handsCount[relToAbsSeat(r)] ?? 13);
      while (oppHands.value[r - 1].length > targetLen) {
        oppHands.value[r - 1].pop();
      }
      while (oppHands.value[r - 1].length < targetLen) {
        oppHands.value[r - 1].push({
          id: `opp-sync-${r}-${oppHands.value[r - 1].length}`,
          type: 'wan',
          value: 1,
          text: '一万',
          color: '#DC2626',
          hidden: true,
        });
      }
    }
  }

  myHandDealtCount.value = myHand.value.length;
  oppHandsDealtCount.value = [oppCounts.value[0], oppCounts.value[1], oppCounts.value[2]];

  const resolvedExpiresAt = msg.settlementExpiresAt || bs?.settlement?.settlementExpiresAt || null;

  if (bs?.phase === 'ended' && bs?.isDraw) {
    if (isDrawStalemate.value) {
      if (Array.isArray(bs?.settlement?.netCoinsPerSeat) && bs.settlement.netCoinsPerSeat.length === 4) {
        stalemateNetCoins.value = [0, 1, 2, 3].map((relS) => bs.settlement.netCoinsPerSeat[relToAbsSeat(relS)] || 0);
      }
      startSettlementCountdown(resolvedExpiresAt, msg.serverTs);
      saveLocalSnapshotNow();
      return;
    }
    triggerDrawStalemate(bs?.settlement?.message || '牌墙抓毕，本局荒庄（流局）！', true, {
      ...(bs.settlement || {}),
      settlementExpiresAt: resolvedExpiresAt,
      serverTs: msg.serverTs,
    });
    return;
  }

  if (bs?.phase === 'ended' && bs?.winnerSeat !== null && bs?.winnerSeat !== undefined) {
    const stlObj = bs.settlement || {};
    const relWinner = absToRelSeat(bs.winnerSeat);
    if (hasWon.value && showSettlement.value) {
      // 已被本地快照抢先恢复，补充权威数据与服务器倒计时
      if (Array.isArray(stlObj.winningHand) && stlObj.winningHand.length > 0) {
        const cleanAuthHand = sortHand(stlObj.winningHand.filter((t: any) => t && !t.hidden));
        if (cleanAuthHand.length > 0) {
          winningBreakdown.value.winningHand = cleanAuthHand;
          if (relWinner === 0) myHand.value = cleanAuthHand;
          else oppHands.value[relWinner - 1] = cleanAuthHand;
        }
      }
      if (Array.isArray(stlObj.winningMelds)) {
        const normAuthMelds = normalizeServerMelds(stlObj.winningMelds).map((m) => sortHand(m));
        winningBreakdown.value.winningMelds = normAuthMelds;
        if (relWinner === 0) meldedTiles.value = normAuthMelds;
        else oppMelds.value[relWinner - 1] = normAuthMelds;
      }
      if (Array.isArray(stlObj.netCoinsPerSeat) && stlObj.netCoinsPerSeat.length === 4) {
        const syncedNet = [0, 1, 2, 3].map((relS) => stlObj.netCoinsPerSeat[relToAbsSeat(relS)] || 0);
        winningBreakdown.value.netCoinsPerSeat = syncedNet;
        winningBreakdown.value.rewardCoins = syncedNet[0];
      }
      winningBreakdown.value.winnerSeat = relWinner;
      winningBreakdown.value.winnerNameSnapshot = SEAT_NAMES[relWinner];
      startSettlementCountdown(resolvedExpiresAt, msg.serverTs);
      saveLocalSnapshotNow();
      drawTable();
      return;
    }
    triggerMahjongWin(relWinner, Boolean(stlObj.isFourHongzhong || stlObj.isFourHz), {
      ...stlObj,
      winningHand: stlObj.winningHand,
      winningMelds: stlObj.winningMelds,
      settlementExpiresAt: resolvedExpiresAt,
      serverTs: msg.serverTs,
      fromServerWs: true,
    });
    return;
  }

  const cw = bs?.extraState?.claimWindow;
  const hasPendingClaimWindow = Boolean(
    !isServerEnded &&
    cw &&
    Array.isArray(cw.pendingSeats) &&
    cw.pendingSeats.some((ps: number) => !Array.isArray(cw.passedSeats) || !cw.passedSeats.includes(ps))
  );

  if (hasPendingClaimWindow) {
    isWaitingServerClaimWindow.value = true;
    const isMyClaimPending =
      cw.pendingSeats.includes(mySeat.value) &&
      (!Array.isArray(cw.passedSeats) || !cw.passedSeats.includes(mySeat.value));
    if (isMyClaimPending && (canPeng.value || canGang.value)) {
      tipMessage.value = '🔄 牌桌状态已恢复！您可以选择碰/杠或过牌';
      drawTable();
      saveLocalSnapshot();
      waitForPlayerClaim(() => handlePassClaim());
      return;
    }
    tipMessage.value = '🀄 牌桌状态已同步，等待其他牌友确认碰/杠...';
    drawTable();
    saveLocalSnapshot();
    return;
  }

  if (!isServerEnded && turnPlayer.value === 0 && myHand.value.length % 3 === 1 && pendingPengTurn.value === null) {
    drawTable();
    saveLocalSnapshot();
    handleDraw();
    return;
  }

  tipMessage.value = turnPlayer.value === 0 ? '🔄 牌桌状态已同步！轮到您出牌' : `🔄 牌桌状态已同步！当前轮到【${SEAT_NAMES[turnPlayer.value]}】出牌`;
  if (turnPlayer.value === 0 && !isClaiming.value) {
    if (isAutoHost.value) {
      checkAutoHostTurn();
    } else {
      startTurnCountdown();
    }
  }
  drawTable();
  saveLocalSnapshot();
}

function dealNewMatch(designatedDealer?: number, forceReApply = false, dealOverride?: any) {
  stopSettlementCountdown();
  hasClickedReady.value = false;
  lastOptimisticDrawId = null;
  lastOptimisticSuppTileId = null;
  isOnlineHuPending = false;
  pendingOnlineGangBackup = null;
  lastDrawnTileIdByRelSeat.fill(null);
  resetAutoHostState();
  const currentDeal = dealOverride || props.initialDeal;
  if (currentDeal?.seatCards) {
    const dealIdentifier = currentDeal.dealId || currentDeal.createdAt || (Array.isArray(currentDeal.seatCards[0]) ? currentDeal.seatCards[0].map((c: any) => c.id || c.name).join('-') : '');
    if (!forceReApply && lastAppliedDealId.value && lastAppliedDealId.value === dealIdentifier && myHand.value && myHand.value.length > 0) {
      console.log(`🛡️ [麻将网络波动防重刷] 发牌 ${dealIdentifier} 已在桌上执行，跳过重复洗牌与重置`);
      return;
    }
    lastAppliedDealId.value = dealIdentifier;
  }
  cleanAllGameTimers();
  hasWon.value = false;
  isDrawStalemate.value = false;
  showSettlement.value = false;

  const isContinuing = designatedDealer !== undefined;
  isDealerContinuing.value = isContinuing;
  let targetDealer = 0;

  const rawDealTiles = currentDeal?.seatCards;
  const hasValidTiles = rawDealTiles && (Array.isArray(rawDealTiles) ? rawDealTiles.length === 4 : (rawDealTiles[0] && rawDealTiles[1] && rawDealTiles[2] && rawDealTiles[3]));
  if (currentDeal && hasValidTiles) {
    const sMe = mySeat.value;
    const sEast = (sMe + 1) % 4;
    const sNorth = (sMe + 2) % 4;
    const sWest = (sMe + 3) % 4;

    myHand.value = sortHand([...(currentDeal.seatCards[sMe] || [])]);
    oppHands.value = [
      sortHand([...(currentDeal.seatCards[sEast] || [])]),
      sortHand([...(currentDeal.seatCards[sNorth] || [])]),
      sortHand([...(currentDeal.seatCards[sWest] || [])]),
    ];
    wallPool = [...(currentDeal.remainingWall || [])];
    wallCount.value = wallPool.length;

    const rawDealer = currentDeal.dealerSeat !== undefined ? currentDeal.dealerSeat : 0;
    targetDealer = (rawDealer - sMe + 4) % 4;
    dealerSeat.value = targetDealer;
    dealerDice.value = currentDeal.dice || { d1: 1, d2: 1, sum: 2 };
  } else {
    // 联机纯真人模式严禁客户端本地洗牌发假牌与人机补位，向服务器请求权威发牌
    if (!props.isPracticeMode) {
      console.log('🛡️ [麻将联机纯真人] 等待服务端权威发牌，禁止本地离线随机发牌与人机补位');
      if (props.ws && props.ws.readyState === WebSocket.OPEN && props.roomId) {
        try {
          props.ws.send(JSON.stringify({
            type: 'request_game_deal',
            roomId: props.roomId,
            gameType: 'mahjong',
            sceneMode: props.sceneMode,
          }));
        } catch {}
      }
      return;
    }

    // Standard Mahjong: strictly 136 tiles in total (36 wan + 36 tiao + 36 tong + 28 zi) - 仅限单机试玩
    wallPool = createTilePool();

    // 掷色子：
    // 两个骰子 1..6，点数 2..12
    const d1 = Math.floor(Math.random() * 6) + 1;
    const d2 = Math.floor(Math.random() * 6) + 1;
    const sum = d1 + d2;

    // 规则：继续游戏时，上局谁自摸谁坐庄率先开牌出牌！若重新开局则由色子定庄
    targetDealer = designatedDealer !== undefined ? designatedDealer : (sum - 1) % 4; // 0=本席(南), 1=东席, 2=北席, 3=西席
    dealerSeat.value = targetDealer;
    dealerDice.value = { d1, d2, sum };

    // 庄家抓 14 张牌，闲家各抓 13 张牌 (起手共 53 张牌)
    if (targetDealer === 0) {
      myHand.value = sortHand(wallPool.slice(0, 14));
      oppHands.value = [
        sortHand(wallPool.slice(14, 27)), // East: 13
        sortHand(wallPool.slice(27, 40)), // North: 13
        sortHand(wallPool.slice(40, 53)), // West: 13
      ];
    } else if (targetDealer === 1) {
      myHand.value = sortHand(wallPool.slice(0, 13));
      oppHands.value = [
        sortHand(wallPool.slice(13, 27)), // East (Dealer): 14
        sortHand(wallPool.slice(27, 40)), // North: 13
        sortHand(wallPool.slice(40, 53)), // West: 13
      ];
    } else if (targetDealer === 2) {
      myHand.value = sortHand(wallPool.slice(0, 13));
      oppHands.value = [
        sortHand(wallPool.slice(13, 26)), // East: 13
        sortHand(wallPool.slice(26, 40)), // North (Dealer): 14
        sortHand(wallPool.slice(40, 53)), // West: 13
      ];
    } else {
      myHand.value = sortHand(wallPool.slice(0, 13));
      oppHands.value = [
        sortHand(wallPool.slice(13, 26)), // East: 13
        sortHand(wallPool.slice(26, 39)), // North: 13
        sortHand(wallPool.slice(39, 53)), // West (Dealer): 14
      ];
    }

    // Remaining in wall: exactly 136 - 14 - 39 = 83 tiles!
    wallPool = wallPool.slice(53);
    wallCount.value = wallPool.length;
  }

  playerKongExtra.value = [0, 0, 0];
  seatKongScores.value = [0, 0, 0, 0];
  stalemateNetCoins.value = [0, 0, 0, 0];
  kongRecords.value = [];
  meldedTiles.value = [];
  oppMelds.value = [[], [], []];
  discards.value = [];
  lastDiscarded.value = null;
  pendingPengTurn.value = null;
  selectedIndex.value = null;
  hasWon.value = false;
  announcingWinnerSeat.value = null;
  winningBreakdown.value = {
    fans: [],
    totalFan: 1,
    rewardCoins: 0,
    maCount: baseMaCount.value,
    rewardMaTiles: [],
    hitMaCount: 0,
    payoutPerOpponent: 10,
    isFourHongzhong: false,
    netCoinsPerSeat: [0, 0, 0, 0],
  };
  if (announceTimer) {
    clearTimeout(announceTimer);
    announceTimer = null;
  }

  // 初始发牌动效阶段：3D 色盅与双色子定庄
  dealingPhase.value = 'rolling_dice';
  isRollingDice.value = true;
  myHandDealtCount.value = 0;
  oppHandsDealtCount.value = [0, 0, 0];
  handFlipProgress.value = 0;
  turnPlayer.value = targetDealer;
  tipMessage.value = isContinuing
    ? `🎲 【${SEAT_NAMES[targetDealer]}】上局自摸继续坐庄，开局掷色定开门...`
    : '🎲 开局掷双色子定庄开门...';
  drawTable();
}

function startDealingTilesAnimation() {
  dealingPhase.value = 'dealing_tiles';
  tipMessage.value = '🀄 开局起手抓牌 (牌背朝上发牌)...';
  drawTable();

  // Wave 1: 4 cards each
  const t1 = setTimeout(() => {
    myHandDealtCount.value = 4;
    oppHandsDealtCount.value = [4, 4, 4];
    soundFX.playCardSnap();
    tipMessage.value = '🀄 抓第一道手牌 (各得 4 张)...';
    drawTable();

    // Wave 2: 8 cards each
    const t2 = setTimeout(() => {
      myHandDealtCount.value = 8;
      oppHandsDealtCount.value = [8, 8, 8];
      soundFX.playCardSnap();
      tipMessage.value = '🀄 抓第二道手牌 (各得 8 张)...';
      drawTable();

      // Wave 3: 12 cards each
      const t3 = setTimeout(() => {
        myHandDealtCount.value = 12;
        oppHandsDealtCount.value = [12, 12, 12];
        soundFX.playCardSnap();
        tipMessage.value = '🀄 抓第三道手牌 (各得 12 张)...';
        drawTable();

        // Wave 4: 13 cards each
        const t4 = setTimeout(() => {
          myHandDealtCount.value = 13;
          oppHandsDealtCount.value = [13, 13, 13];
          soundFX.playCardSnap();
          tipMessage.value = '🀄 抓第四道起手牌 (各得 13 张)...';
          drawTable();

          // Wave 5: Dealer draws 14th tile (庄家跳牌)
          const t5 = setTimeout(() => {
            if (dealerSeat.value === 0) {
              myHandDealtCount.value = 14;
            } else {
              oppHandsDealtCount.value[dealerSeat.value - 1] = 14;
            }
            soundFX.playMahjongClack();
            tipMessage.value = `🀄 【${SEAT_NAMES[dealerSeat.value]}】跳牌抓第 14 张起手牌！`;
            drawTable();

            // Wave 6: Flip & Sort Hand (翻面亮牌理顺)
            const t6 = setTimeout(() => {
              startHandFlipAnimation();
            }, 360);
            dealingAnimTimers.push(t6);
          }, 320);
          dealingAnimTimers.push(t5);
        }, 300);
        dealingAnimTimers.push(t4);
      }, 300);
      dealingAnimTimers.push(t3);
    }, 300);
    dealingAnimTimers.push(t2);
  }, 220);
  dealingAnimTimers.push(t1);
}

function startHandFlipAnimation() {
  dealingPhase.value = 'sorting_tiles';
  tipMessage.value = '🀄 整牌理牌，翻面亮牌...';
  soundFX.playMahjongClack();
  const startTime = performance.now();
  const duration = 420;

  function step(now: number) {
    const elapsed = now - startTime;
    const p = Math.min(1, elapsed / duration);
    handFlipProgress.value = p;
    drawTable();
    if (p < 1) {
      flipRafId = requestAnimationFrame(step);
    } else {
      flipRafId = null;
      finishDealingAndStartPlay();
    }
  }
  flipRafId = requestAnimationFrame(step);
}

function finishDealingAndStartPlay() {
  dealingPhase.value = 'playing';
  turnPlayer.value = dealerSeat.value;
  drawTable();
  saveLocalSnapshot();

  // 特殊规则：起手摸齐四张红中直接结束游戏！
  if (isHongzhongMode.value) {
    if (myHand.value.filter(isHongzhong).length === 4) {
      const t = setTimeout(() => {
        if (hasWon.value || showSettlement.value || dealingPhase.value === 'settled') return;
        submitOrTriggerMyHu(true);
      }, 350);
      dealingAnimTimers.push(t);
      return;
    }
    if (props.isPracticeMode) {
      for (let b = 0; b < 3; b++) {
        if (oppHands.value[b].filter(isHongzhong).length === 4) {
          const t = setTimeout(() => {
            if (hasWon.value || showSettlement.value || dealingPhase.value === 'settled') return;
            triggerMahjongWin(b + 1, true);
          }, 350);
          dealingAnimTimers.push(t);
          return;
        }
      }
    }
  }

  if (dealerSeat.value === 0) {
    tipMessage.value = isDealerContinuing.value
      ? '👑 您上局自摸连庄，起手执14张牌，请点击手牌出第一张牌！'
      : (isHongzhongMode.value
        ? '👑 您是庄家，手牌理顺就绪，请先出牌！(红中百搭万能)'
        : '👑 您是庄家，起手抓齐 14 张，请点击手牌选一张打出');
    drawTable();
    if (isAutoHost.value) {
      checkAutoHostTurn();
    } else {
      startTurnCountdown();
    }
  } else {
    tipMessage.value = isDealerContinuing.value
      ? `👑 【${SEAT_NAMES[dealerSeat.value]}】上局自摸连庄，执 14 张牌率先出牌...`
      : `👑 【${SEAT_NAMES[dealerSeat.value]}】是庄家，执 14 张牌率先出牌...`;
    drawTable();
    const dealerAbsSeat = relToAbsSeat(dealerSeat.value);
    if (props.isPracticeMode && isSeatBot(dealerAbsSeat)) {
      const t = setTimeout(() => {
        botPlay(dealerSeat.value, () => passTurnToNext(dealerSeat.value));
      }, 950);
      dealingAnimTimers.push(t);
    }
  }
}

function skipDealingAnimation() {
  cleanDealingTimers();
  isRollingDice.value = false;
  if (dealerDice.value) {
    currentDiceDisplay.value = { d1: dealerDice.value.d1, d2: dealerDice.value.d2 };
  }
  myHandDealtCount.value = myHand.value.length;
  oppHandsDealtCount.value = [oppHands.value[0].length, oppHands.value[1].length, oppHands.value[2].length];
  handFlipProgress.value = 1;
  dealingPhase.value = 'playing';
  saveLocalSnapshot();

  // Check 4 Hongzhong instant win
  if (isHongzhongMode.value) {
    if (myHand.value.filter(isHongzhong).length === 4) {
      const t = setTimeout(() => {
        if (hasWon.value || showSettlement.value || dealingPhase.value === 'settled') return;
        submitOrTriggerMyHu(true);
      }, 350);
      dealingAnimTimers.push(t);
      return;
    }
    if (props.isPracticeMode) {
      for (let b = 0; b < 3; b++) {
        if (oppHands.value[b].filter(isHongzhong).length === 4) {
          const t = setTimeout(() => {
            if (hasWon.value || showSettlement.value || dealingPhase.value === 'settled') return;
            triggerMahjongWin(b + 1, true);
          }, 350);
          dealingAnimTimers.push(t);
          return;
        }
      }
    }
  }

  turnPlayer.value = dealerSeat.value;
  if (dealerSeat.value === 0) {
    tipMessage.value = isDealerContinuing.value
      ? '👑 您上局自摸连庄，起手执14张牌，请点击手牌出第一张牌！'
      : (isHongzhongMode.value
        ? '👑 您是庄家，手牌理顺就绪，请先出牌！(红中百搭万能)'
        : '👑 您是庄家，起手抓齐 14 张，请点击手牌选一张打出');
    drawTable();
    if (isAutoHost.value) {
      checkAutoHostTurn();
    } else {
      startTurnCountdown();
    }
  } else {
    tipMessage.value = isDealerContinuing.value
      ? `👑 【${SEAT_NAMES[dealerSeat.value]}】上局自摸连庄，执 14 张牌率先出牌...`
      : `👑 【${SEAT_NAMES[dealerSeat.value]}】是庄家，执 14 张牌率先出牌...`;
    drawTable();
    const dealerAbsSeat = relToAbsSeat(dealerSeat.value);
    if (props.isPracticeMode && isSeatBot(dealerAbsSeat)) {
      const t = setTimeout(() => {
        botPlay(dealerSeat.value, () => passTurnToNext(dealerSeat.value));
      }, 800);
      dealingAnimTimers.push(t);
    }
  }
}

// 规则：可以碰 (他人出牌且本家未摸牌时，手牌中有2张相同牌；红中不可碰)
const canPeng = computed(() => {
  if (hasWon.value || showSettlement.value || dealingPhase.value !== 'playing') return false;
  if (myHand.value.length % 3 !== 1) return false;
  if (!lastDiscarded.value || lastDiscarded.value.discardBy === 0) return false;
  const d = lastDiscarded.value;
  // 红中模式下，红中为万能百搭，不可被碰
  if (isHongzhongMode.value && isHongzhong(d)) return false;
  return myHand.value.filter((c) => c.type === d.type && c.value === d.value).length >= 2;
});

// 规则：可以杠 (支持明杠、暗杠、补杠；红中模式下四红中直接胡牌结算)
const canGang = computed(() => {
  if (hasWon.value || showSettlement.value || isDrawStalemate.value || dealingPhase.value !== 'playing') return false;
  const hasWallTiles = isOnlineMultiplayerRoom.value ? wallCount.value > 0 : (wallPool.length > 0 && wallCount.value > 0);
  if (!hasWallTiles) return false;
  // 1. 明杠：他人出牌且本家未摸牌时，手中已有3张相同牌
  if (myHand.value.length % 3 === 1 && lastDiscarded.value && lastDiscarded.value.discardBy !== 0) {
    const d = lastDiscarded.value;
    if (isHongzhongMode.value && isHongzhong(d)) return false;
    if (myHand.value.filter((c) => c.type === d.type && c.value === d.value).length === 3) return true;
  }
  // 2. 暗杠与补杠：轮到自己抓牌后 (手牌数为 3k+2)
  if (turnPlayer.value === 0 && myHand.value.length % 3 === 2) {
    // 暗杠：手牌中有4张完全相同的牌 (红中场中4张红中直接胡牌，不作为暗杠)
    const counts = new Map<string, number>();
    myHand.value.forEach((c) => {
      if (isHongzhongMode.value && isHongzhong(c)) return;
      counts.set(`${c.type}-${c.value}`, (counts.get(`${c.type}-${c.value}`) || 0) + 1);
    });
    for (const cnt of counts.values()) {
      if (cnt === 4) return true;
    }
    // 补杠：摸到已碰明刻中对应牌型的第4张
    for (const meld of meldedTiles.value) {
      if (meld.length === 3 && meld[0].type === meld[1].type && meld[0].value === meld[1].value) {
        if (myHand.value.some((c) => c.type === meld[0].type && c.value === meld[0].value)) {
          return true;
        }
      }
    }
  }
  return false;
});



// 规则：不可炮胡，仅限自摸 (必须轮到自己回合且已抓牌在手，手牌数为 3k+2)
const canHu = computed(() => {
  if (hasWon.value || showSettlement.value || dealingPhase.value !== 'playing') return false;
  const totalCount = myHand.value.length + meldedTiles.value.length * 3;
  if (turnPlayer.value !== 0 || myHand.value.length % 3 !== 2 || totalCount !== 14) return false;
  if (isHongzhongMode.value) {
    return checkHongzhongWin(myHand.value);
  }
  return checkMahjongWin(myHand.value);
});

/**
 * 统一麻将胡牌与积分结算
 * - 普通六码/十码场：自摸奖 6 / 10 码，底分10分，一五九码及红中东风每中1码+10分
 * - 红中六码/十码场：
 *   1. 摸到4张红中直接结束游戏：
 *      - 六码场：等于六码全中加底分，各得其他玩家70分，共210分
 *      - 十码场：等于十码全中加底分，各得其他玩家110分，共330分
 *   2. 自摸手牌没有红中：奖励额外加2个码 (六码场变8码，十码场变12码)
 */
function triggerDrawStalemate(message = '牌墙已摸空，荒牌流局！', fromServerWs = false, payloadData?: any) {
  if (isDrawStalemate.value || hasWon.value || showSettlement.value || dealingPhase.value === 'settled') return;
  cleanAllGameTimers();
  isClaiming.value = false;
  pendingPengTurn.value = null;
  wallCount.value = 0;
  isDrawStalemate.value = true;
  dealingPhase.value = 'settled';

  const relKongScores = [0, 1, 2, 3].map((relS) => {
    const absS = relToAbsSeat(relS);
    if (Array.isArray(payloadData?.netCoinsPerSeat) && typeof payloadData.netCoinsPerSeat[absS] === 'number') {
      return payloadData.netCoinsPerSeat[absS];
    }
    if (Array.isArray(payloadData?.seatKongScores) && typeof payloadData.seatKongScores[absS] === 'number') {
      return payloadData.seatKongScores[absS];
    }
    return seatKongScores.value[relS] || 0;
  });
  seatKongScores.value = [...relKongScores];
  stalemateNetCoins.value = [...relKongScores];
  if (Array.isArray(payloadData?.kongHistory)) {
    kongRecords.value = formatKongHistoryRecords(payloadData.kongHistory);
  }

  const myKongNet = relKongScores[0] || 0;
  const kongNote = myKongNet !== 0 ? `（本局杠分 ${myKongNet > 0 ? `+${myKongNet}` : myKongNet} 筹码已结算）` : '';
  tipMessage.value = `${message}${kongNote}`;

  emit('move', {
    action: 'draw_stalemate',
    seat: mySeat.value,
    isRemote: Boolean(fromServerWs),
    skipWs: true,
    winner: -1,
    rewardCoins: myKongNet,
    detail: tipMessage.value,
  });

  startSettlementCountdown(payloadData?.settlementExpiresAt, payloadData?.serverTs);
  saveLocalSnapshotNow();
  emit('game-over', { gameType: 'mahjong', isDraw: true, rewardCoins: myKongNet });
  if (!fromServerWs && isOnlineMultiplayerRoom.value) {
    sendGameMove('draw_stalemate', { absSeat: mySeat.value });
  }
  drawTable();
}

function triggerMahjongWin(winnerSeat: number, isFourHongzhong = false, payloadData?: any) {
  if (hasWon.value || showSettlement.value || dealingPhase.value === 'settled') {
    console.warn('🛡️ [麻将结算防重] 已在结算状态，忽略重复调用与重复扣分');
    return;
  }

  const isPlayer = winnerSeat === 0;
  // 1. 同步全网权威胡牌手牌与亮副，剔除遮罩占位牌并严格按花色点数理顺排序
  const serverUnmaskedHand = Array.isArray(payloadData?.winningHand)
    ? payloadData.winningHand.filter((t: any) => t && !t.hidden)
    : [];
  const rawLocalHand = isPlayer ? myHand.value : (oppHands.value[winnerSeat - 1] || []);
  let winnerHand = serverUnmaskedHand.length > 0 ? sortHand(serverUnmaskedHand) : sortHand(rawLocalHand);

  const rawMelds = Array.isArray(payloadData?.winningMelds)
    ? normalizeServerMelds(payloadData.winningMelds)
    : normalizeServerMelds(isPlayer ? meldedTiles.value : (oppMelds.value[winnerSeat - 1] || []));
  const winnerMeldsSorted = rawMelds.map((m) => sortHand(m));

  // 防炸胡本地守卫：非服务端下发的本地胡牌，必须严格满足 3k+2、副露+手牌=14张，且通过胡牌/四红中检测
  if (!payloadData?.fromServerWs) {
    const totalCountLocal = winnerHand.length + winnerMeldsSorted.length * 3;
    const hzCntLocal = isHongzhongMode.value ? winnerHand.filter(isHongzhong).length : 0;
    const isFourHzLocal = isHongzhongMode.value && hzCntLocal >= 4;
    const isLegalWinLocal = isHongzhongMode.value ? checkHongzhongWin(winnerHand) : checkMahjongWin(winnerHand);
    if (winnerHand.length % 3 !== 2 || totalCountLocal !== 14 || (!isFourHzLocal && !isLegalWinLocal)) {
      console.warn(`🛡️ [客户端防炸胡拦截] 席位 ${winnerSeat} 手牌(${winnerHand.length})+副露(${winnerMeldsSorted.length})=${totalCountLocal} 不符合自摸胡牌条件，拒绝结算！`);
      return;
    }
  }

  cleanAllGameTimers();

  hasWon.value = true;
  showSettlement.value = true;
  dealingPhase.value = 'settled';
  const winnerName = SEAT_NAMES[winnerSeat] || ['您 (本席)', '东席 (牌友)', '北席 (牌友)', '西席 (牌友)'][winnerSeat] || '雀圣';

  if (serverUnmaskedHand.length > 0 || !isPlayer) {
    if (isPlayer) {
      myHand.value = winnerHand;
      if (Array.isArray(payloadData?.winningMelds)) {
        meldedTiles.value = winnerMeldsSorted;
      }
    } else {
      oppHands.value[winnerSeat - 1] = winnerHand;
      if (Array.isArray(payloadData?.winningMelds)) {
        oppMelds.value[winnerSeat - 1] = winnerMeldsSorted;
      }
    }
  } else {
    myHand.value = winnerHand;
  }

  const hzCountInHand = winnerHand.filter(isHongzhong).length;
  const baseCodes = baseMaCount.value; // 6 or 10
  const isHzMode = isHongzhongMode.value;
  const fourHzWin = isHzMode && (isFourHongzhong || hzCountInHand === 4 || Boolean(payloadData?.isFourHz));

  // Sync authoritative kong scores & history if server sent them
  if (Array.isArray(payloadData?.seatKongScores) && payloadData.seatKongScores.length === 4) {
    for (let relS = 0; relS < 4; relS++) {
      seatKongScores.value[relS] = payloadData.seatKongScores[relToAbsSeat(relS)] || 0;
    }
  }
  if (Array.isArray(payloadData?.kongHistory)) {
    kongRecords.value = formatKongHistoryRecords(payloadData.kongHistory);
  }

  // Ensure isYiWuJiuMa uses current round's fourHzWin rather than any stale previous-round flag
  winningBreakdown.value = {
    ...winningBreakdown.value,
    isFourHongzhong: fourHzWin,
  };

  let maCount = baseCodes;
  let hitCount = 0;
  let payoutPerOpponent = 0;
  let fansList: string[] = [];
  let rewardMaTiles: MahjongTile[] = [];

  // 2. 如果收到服务端/发起方的权威抓码结果，直接采用权威数据，保证全服各机呈现完全相同开码与分值！
  if (payloadData?.maTiles && Array.isArray(payloadData.maTiles) && payloadData.maTiles.length > 0) {
    rewardMaTiles = payloadData.maTiles;
    maCount = payloadData.maCount || rewardMaTiles.length;
    hitCount = payloadData.hitMaCount !== undefined ? payloadData.hitMaCount : rewardMaTiles.filter(isYiWuJiuMa).length;
    payoutPerOpponent = payloadData.payoutPerOpponent !== undefined ? payloadData.payoutPerOpponent : (10 + hitCount * 10);
    if (fourHzWin) {
      fansList = [
        '摸到四张红中 · 天降大满贯！',
        baseCodes === 6 ? '红中六码全中 (奖60分)' : '红中十码全中 (奖100分)',
        '各得其他玩家底分10分',
      ];
    } else {
      const hasNoHongzhong = isHzMode && hzCountInHand === 0;
      if (hasNoHongzhong) {
        fansList.push(`自摸和牌 (手牌无红中奖励加2码，共${maCount}码)`);
      } else {
        fansList.push(isHzMode ? `红中百搭自摸和牌 (奖${maCount}码)` : `自摸和牌 (奖${maCount}码)`);
      }
      fansList.push(`自摸奖码 (${maCount}码中${hitCount}码)`);
    }
  } else if (fourHzWin) {
    // 规则：摸到四张红中直接结束游戏！
    maCount = baseCodes;
    hitCount = baseCodes;
    payoutPerOpponent = 10 + hitCount * 10; // 70 or 110
    fansList = [
      '摸到四张红中 · 天降大满贯！',
      baseCodes === 6 ? '红中六码全中 (奖60分)' : '红中十码全中 (奖100分)',
      '各得其他玩家底分10分',
    ];

    rewardMaTiles = wallPool.slice(0, Math.min(maCount, wallPool.length));
    while (rewardMaTiles.length < maCount) {
      rewardMaTiles.push({
        id: `hz-win-${rewardMaTiles.length}`,
        type: 'zi',
        value: 5,
        text: '红中',
        color: '#DC2626',
        isRedDragon: true,
      });
    }
  } else {
    // 普通自摸 / 红中自摸
    const hasNoHongzhong = isHzMode && hzCountInHand === 0;
    if (hasNoHongzhong) {
      maCount = baseCodes + 2;
      fansList.push(`自摸和牌 (手牌无红中奖励加2码，共${maCount}码)`);
    } else {
      maCount = baseCodes;
      fansList.push(isHzMode ? `红中百搭自摸和牌 (奖${maCount}码)` : `自摸和牌 (奖${maCount}码)`);
    }

    rewardMaTiles = wallPool.slice(0, Math.min(maCount, wallPool.length));
    if (rewardMaTiles.length < maCount) {
      const extraPool = createTilePool();
      while (rewardMaTiles.length < maCount && extraPool.length > 0) {
        rewardMaTiles.push(extraPool.pop()!);
      }
    }

    hitCount = rewardMaTiles.filter(isYiWuJiuMa).length;
    payoutPerOpponent = 10 + hitCount * 10;
    fansList.push(`自摸奖码 (${maCount}码中${hitCount}码)`);
  }

  // 3. 权威零和算分：优先映射服务端的 netCoinsPerSeat（绝对席位映射至本地相对视角），确保零水损与零分值流失
  const netCoinsPerSeat = [0, 0, 0, 0];
  if (payloadData?.netCoinsPerSeat && Array.isArray(payloadData.netCoinsPerSeat) && payloadData.netCoinsPerSeat.length === 4) {
    for (let relS = 0; relS < 4; relS++) {
      const absS = relToAbsSeat(relS);
      netCoinsPerSeat[relS] = payloadData.netCoinsPerSeat[absS];
    }
  } else {
    for (let s = 0; s < 4; s++) {
      const zimoDelta = s === winnerSeat ? payoutPerOpponent * 3 : -payoutPerOpponent;
      netCoinsPerSeat[s] = zimoDelta + seatKongScores.value[s];
    }
  }

  // 本席玩家 (seat 0) 实际总盈亏
  const rewardCoins = netCoinsPerSeat[0];
  const myKongNet = seatKongScores.value[0] || 0;
  const winnerKongNet = seatKongScores.value[winnerSeat] || 0;
  const kongBonusTotal = Math.max(0, winnerKongNet);
  const opponentPayouts = [0, 0, 0];
  const oppNames = ['东席', '北席', '西席'];

  if (isPlayer) {
    for (let i = 0; i < 3; i++) {
      const oppSeat = i + 1;
      opponentPayouts[i] = Math.max(0, -netCoinsPerSeat[oppSeat]);
      if (seatKongScores.value[oppSeat] !== 0) {
        const kVal = seatKongScores.value[oppSeat];
        fansList.push(`${oppNames[i]}杠分 (${kVal > 0 ? `+${kVal}` : kVal}分)`);
      }
    }
    if (myKongNet > 0) {
      fansList.push(`本席杠牌进账 (+${myKongNet}分)`);
    } else if (myKongNet < 0) {
      fansList.push(`本席被杠扣除 (${myKongNet}分)`);
    }
  } else {
    for (let i = 0; i < 3; i++) {
      const oppSeat = i + 1;
      if (oppSeat === winnerSeat) {
        opponentPayouts[i] = netCoinsPerSeat[oppSeat];
      } else {
        opponentPayouts[i] = Math.max(0, -netCoinsPerSeat[oppSeat]);
      }
    }

    if (myKongNet > 0) {
      for (const rec of kongRecords.value) {
        if (rec.includes('您')) {
          fansList.push(rec);
        }
      }
      const myGrossLoss = payoutPerOpponent;
      if (rewardCoins >= 0) {
        fansList.push(`自摸输${myGrossLoss}分，杠牌进账+${myKongNet}分，净赢+${rewardCoins}分`);
      } else {
        fansList.push(`自摸输${myGrossLoss}分，冲抵杠分+${myKongNet}分，实输${-rewardCoins}分`);
      }
    } else if (myKongNet < 0) {
      fansList.push(`被杠扣除${-myKongNet}分，合局实输${-rewardCoins}分`);
    }

    for (let i = 0; i < 3; i++) {
      const oppSeat = i + 1;
      if (seatKongScores.value[oppSeat] !== 0) {
        const kVal = seatKongScores.value[oppSeat];
        fansList.push(`${oppNames[i]}杠分 (${kVal > 0 ? `+${kVal}` : kVal}分)`);
      }
    }
  }

  winningBreakdown.value = {
    fans: fansList,
    totalFan: 1 + hitCount,
    rewardCoins,
    maCount,
    rewardMaTiles,
    hitMaCount: hitCount,
    payoutPerOpponent,
    isFourHongzhong: fourHzWin,
    kongBonusTotal,
    myKongNet,
    opponentPayouts,
    winnerSeat,
    netCoinsPerSeat: [...netCoinsPerSeat],
    winningHand: [...winnerHand],
    winningMelds: winnerMeldsSorted.map((m) => [...m]),
    winnerNameSnapshot: winnerName,
  };

  lastWinnerSeat.value = winnerSeat;

  soundFX.playVictory();
  if (isPlayer) {
    haptics.sealStamp();
    haptics.victory();
  }

  if (fourHzWin) {
    soundFX.speakAction(isPlayer ? '摸到四张红中！天降大满贯！' : `${winnerName}摸到四张红中胡牌！`);
    tipMessage.value = `🀄 【${winnerName}】摸到四张红中！${baseCodes === 6 ? '六码全中各得70分共210分' : '十码全中各得110分共330分'}，直接结束游戏！`;
  } else {
    if (isPlayer) {
      soundFX.speakAction('zimo', { text: '自摸！胡了！' });
    } else {
      soundFX.speakAction('zimo', { text: `${winnerName}自摸胡牌！` });
    }
    tipMessage.value = `🀄 【${winnerName}】自摸和出！斩获 ${hitCount} 码！`;
  }

  emit('move', {
    action: 'hu',
    seat: relToAbsSeat(winnerSeat),
    isRemote: !isPlayer || Boolean(payloadData?.fromServerWs),
    skipWs: true,
    winner: winnerSeat,
    fan: 1 + hitCount,
    rewardCoins,
    payoutPerOpponent,
    detail: fourHzWin
      ? `${winnerName}摸到四张红中结束游戏 (${baseCodes === 6 ? '70分/各家' : '110分/各家'})`
      : `${winnerName}自摸和牌 (奖${maCount}码中${hitCount}码)`,
  });

  if (winnerSeat === 0 && !payloadData?.fromServerWs) {
    sendGameMove('hu', {
      winner: mySeat.value,
      isFourHz: fourHzWin,
      winningHand: winnerHand,
      winningMelds: winnerMeldsSorted,
      seatKongScores: seatKongScores.value,
      fan: 1 + hitCount,
      rewardCoins,
      payoutPerOpponent,
      absSeat: mySeat.value,
    });
  }

  startSettlementCountdown(payloadData?.settlementExpiresAt, payloadData?.serverTs);
  saveLocalSnapshotNow();
  emit('game-over', { gameType: 'mahjong', winnerSeat, rewardCoins });

  drawTable();
}


let drawRafId: number | null = null;



function syncOpponentsFromBoardState(bs: any) {
  if (!bs || !isOnlineMultiplayerRoom.value) return;
  if (dealingPhase.value !== 'playing' || hasWon.value || showSettlement.value || isDrawStalemate.value) return;
  let changed = false;
  if (Array.isArray(bs.handsCount) && bs.handsCount.length === 4) {
    for (let r = 1; r < 4; r++) {
      const absR = relToAbsSeat(r);
      const targetLen = Number(bs.handsCount[absR]);
      if (Number.isNaN(targetLen) || targetLen < 0) continue;
      const bIdx = r - 1;
      const arr = oppHands.value[bIdx] || [];
      if (arr.length !== targetLen) {
        while (arr.length > targetLen) {
          arr.pop();
        }
        while (arr.length < targetLen) {
          arr.push({
            id: `opp-sync-${r}-${arr.length}-${Date.now()}`,
            type: 'wan',
            value: 1,
            text: '一万',
            color: '#DC2626',
            hidden: true,
          });
        }
        oppHands.value[bIdx] = arr;
        changed = true;
      }
    }
  }
  if (Array.isArray(bs.extraState?.mahjongMelds) && bs.extraState.mahjongMelds.length === 4) {
    for (let r = 1; r < 4; r++) {
      const absR = relToAbsSeat(r);
      const normalized = normalizeServerMelds(bs.extraState.mahjongMelds[absR] || []);
      const cur = oppMelds.value[r - 1] || [];
      const sameLen = cur.length === normalized.length && cur.every((m, idx) => m.length === normalized[idx]?.length);
      if (!sameLen) {
        oppMelds.value[r - 1] = normalized;
        changed = true;
      }
    }
  }
  if (changed) {
    drawTable();
  }
}

function ensureOpponentHandSize(botIdx: number) {
  if (dealingPhase.value !== 'playing' && dealingPhase.value !== 'sorting_tiles') return;
  const oppHand = oppHands.value[botIdx] || [];
  const meldCount = (oppMelds.value[botIdx] || []).length;
  // Standard Mahjong rule: baseline standing hand size is 13 minus meldCount * 3
  if (oppHand.length === 0) {
    const defaultLen = Math.max(1, 13 - (meldCount * 3));
    for (let i = 0; i < defaultLen; i++) {
      oppHand.push({
        id: `opp-ph-${botIdx}-${i}`,
        type: 'wan',
        value: 1,
        text: '一万',
        color: '#DC2626',
        hidden: true,
      });
    }
    oppHands.value[botIdx] = oppHand;
  }
}

function drawTable() {
  if (drawRafId !== null) return;
  drawRafId = requestAnimationFrame(() => {
    drawRafId = null;
    renderTableFrame();
  });
}

function renderTableFrame() {
  const canvas = canvasRef.value;
  if (!canvas) return;
  renderMahjongTableFrame(canvas, {
    tableTheme: props.tableTheme,
    dealingPhase: dealingPhase.value,
    turnPlayer: turnPlayer.value,
    dealerSeat: dealerSeat.value,
    wallCount: wallCount.value,
    isClaiming: isClaiming.value,
    claimCountdown: claimCountdown.value,
    turnCountdown: turnCountdown.value,
    myHand: myHand.value,
    myHandDealtCount: myHandDealtCount.value,
    oppHands: oppHands.value,
    oppHandsDealtCount: oppHandsDealtCount.value,
    oppMelds: oppMelds.value,
    meldedTiles: meldedTiles.value,
    discards: discards.value,
    selectedIndex: selectedIndex.value,
    handFlipProgress: handFlipProgress.value,
    roomPlayers: props.roomPlayers,
    relToAbsSeat,
    isSeatAutoManaged,
    isSeatDisconnected,
    ensureOpponentHandSize,
  });
}

function onCanvasClick(e: MouseEvent | PointerEvent) {
  if (dealingPhase.value !== 'playing') return;
  const canvas = canvasRef.value;
  if (!canvas) return;
  const { x, y } = getCanvasEventCoords(canvas, e.clientX, e.clientY, props.isRotated90);

  const handLen = myHand.value.length;
  const handTileW = 38;
  const handTileH = 56;
  const gap = 3;
  const hasDrawn14 = handLen % 3 === 2;
  const drawnOffset = hasDrawn14 ? 14 : 0;
  const handTotalW = handLen > 0 ? (handLen * handTileW + (handLen - 1) * gap + drawnOffset) : 0;

  const meldTileW = 28;
  const meldGap = 2;
  const meldSetGap = 12;
  const meldsToHandGap = 18;
  const meldStartX = 24;

  let totalMeldsW = 0;
  if (meldedTiles.value.length > 0) {
    meldedTiles.value.forEach((set, sIdx) => {
      totalMeldsW += set.length * meldTileW + (set.length - 1) * meldGap;
      if (sIdx < meldedTiles.value.length - 1) totalMeldsW += meldSetGap;
    });
  }

  let handStartX = 24;
  if (totalMeldsW > 0) {
    handStartX = meldStartX + totalMeldsW + meldsToHandGap;
  } else {
    handStartX = Math.max(20, Math.floor((680 - handTotalW) / 2));
  }
  const baseY = 430 - 142; // 288，与 drawTable 严格同步，点击判定精准无偏差

  for (let i = 0; i < handLen; i++) {
    const isDrawn14 = i === handLen - 1 && hasDrawn14;
    const extraOffset = isDrawn14 ? 14 : 0;
    const tx = handStartX + i * (handTileW + gap) + extraOffset;

    if (x >= tx && x <= tx + handTileW && y >= baseY + 40 - 20 && y <= baseY + 40 + handTileH) {
      if (selectedIndex.value === i) {
        // Discard tile!
        if (turnPlayer.value !== 0 || myHand.value.length % 3 !== 2) return;
        stopTurnCountdown();
        pendingPengTurn.value = null;
        const discarded = myHand.value.splice(i, 1)[0];
        discarded.discardBy = 0;
        myHand.value = sortHand(myHand.value);
        discards.value.push(discarded);
        lastDiscarded.value = discarded;
        soundFX.playMahjongClack();
        haptics.mahjongClack();
        selectedIndex.value = null;
        tipMessage.value = `打出了【${discarded.text}】到牌池`;
        soundFX.speakVoice(discarded.text);
        drawTable();
        emit('move', { action: 'discard', tile: discarded });
        sendGameMove('discard', { tile: discarded, absSeat: mySeat.value });
        saveLocalSnapshot();

        // Trigger bot or network turns
        passTurnToNext(0);
        return;
      } else {
        selectedIndex.value = i;
        soundFX.playTap();
        haptics.tap();
        drawTable();
        return;
      }
    }
  }
}

function handleDraw() {
  if (hasWon.value || showSettlement.value || isDrawStalemate.value || dealingPhase.value !== 'playing') return;
  if (myHand.value.length % 3 !== 1) return;
  turnPlayer.value = 0;
  if (isOnlineMultiplayerRoom.value && props.ws && props.ws.readyState === WebSocket.OPEN) {
    if (wallCount.value <= 0) {
      sendGameMove('draw', { absSeat: mySeat.value, wallRemaining: 0 });
      return;
    }
    if (isOnlineDrawPending) return;
    isOnlineDrawPending = true;
    armOnlinePendingWatchdog();
    tipMessage.value = '🀄 正在从牌墙抓牌...';
    drawTable();
    sendGameMove('draw', { absSeat: mySeat.value });
    return;
  }
  if (wallPool.length === 0) {
    triggerDrawStalemate('牌墙已摸空，荒牌流局！', !isOnlineMultiplayerRoom.value);
    return;
  }
  const newTile = wallPool.shift()!;
  wallCount.value = wallPool.length;
  myHand.value.push(newTile);
  lastOptimisticDrawId = newTile.id;
  lastDrawnTileIdByRelSeat[0] = newTile.id;
  turnPlayer.value = 0;
  sendGameMove('draw', { absSeat: mySeat.value, tile: newTile, wallRemaining: wallPool.length });
  saveLocalSnapshot();
  soundFX.playMahjongClack();
  haptics.mahjongClack();

  // 特殊规则：摸到四张红中直接结束游戏！
  if (isHongzhongMode.value && myHand.value.filter(isHongzhong).length === 4) {
    drawTable();
    submitOrTriggerMyHu(true);
    return;
  }

  tipMessage.value = `摸到了【${newTile.text}】${isHongzhong(newTile) && isHongzhongMode.value ? '(红中百搭万能)' : ''}，请自摸胡牌或点击出牌`;
  drawTable();
  if (isAutoHost.value) {
    checkAutoHostTurn();
  } else {
    startTurnCountdown();
  }
}

function handleDiscardSelected() {
  if (selectedIndex.value === null || myHand.value.length % 3 !== 2 || turnPlayer.value !== 0 || dealingPhase.value !== 'playing') return;
  stopTurnCountdown();
  pendingPengTurn.value = null;
  const discarded = myHand.value.splice(selectedIndex.value, 1)[0];
  discarded.discardBy = 0;
  myHand.value = sortHand(myHand.value);
  discards.value.push(discarded);
  lastDiscarded.value = discarded;
  soundFX.playMahjongClack();
  haptics.mahjongClack();
  selectedIndex.value = null;
  tipMessage.value = `打出了【${discarded.text}】到牌池`;
  soundFX.speakVoice(discarded.text);
  drawTable();
  emit('move', { action: 'discard', tile: discarded });
  sendGameMove('discard', { tile: discarded, absSeat: mySeat.value });
  saveLocalSnapshot();
  passTurnToNext(0);
}

// 规则：可以碰 (他人出牌时，从手牌中拿出2张相同牌与他人弃牌组成明刻)
function handlePeng() {
  if (!canPeng.value || !lastDiscarded.value) return;
  if (myHand.value.length % 3 !== 1) return;
  stopClaimCountdown();
  isClaiming.value = false;
  if (botTimer) clearTimeout(botTimer);

  const d = lastDiscarded.value;
  const matches = myHand.value.filter((c) => c.type === d.type && c.value === d.value).slice(0, 2);

  if (matches.length === 2) {
    const rmIds = new Set(matches.map((c) => c.id));
    myHand.value = myHand.value.filter((c) => !rmIds.has(c.id));
    meldedTiles.value.push([...matches, d]);
    // Remove the claimed tile from discards so total tiles remain exact
    const dIdx = discards.value.findIndex((c) => c.id === d.id);
    if (dIdx >= 0) {
      discards.value.splice(dIdx, 1);
    } else if (discards.value.length > 0 && discards.value[discards.value.length - 1].type === d.type && discards.value[discards.value.length - 1].value === d.value) {
      discards.value.pop();
    }
    lastDiscarded.value = null;
    pendingPengTurn.value = 0;
    turnPlayer.value = 0;
    selectedIndex.value = null;
    soundFX.playMahjongClack();
    haptics.sealStamp();
    soundFX.speakAction('peng');
    tipMessage.value = `碰牌成功！碰进【${d.text}】亮刻，请打出一张牌`;
    drawTable();
    sendGameMove('peng', { tile: d, targetDiscardId: d.id, absSeat: mySeat.value });
    saveLocalSnapshot();
    if (isAutoHost.value) {
      checkAutoHostTurn();
    } else {
      startTurnCountdown();
    }
  }
}

// 规则：可以杠 (明杠、暗杠、补杠，杠后从牌墙末尾补一张牌入座)
function handleGang() {
  const hasWallTiles = isOnlineMultiplayerRoom.value ? wallCount.value > 0 : (wallPool.length > 0 && wallCount.value > 0);
  if (!canGang.value || !hasWallTiles) return;
  stopClaimCountdown();
  isClaiming.value = false;
  if (botTimer) clearTimeout(botTimer);

  let kongedOpponentSeat: number | null = null;
  let resolvedGangType: 'ming' | 'an' | 'bu' | null = null;
  let gangTile: MahjongTile | null = null;

  // Case 1: 明杠 (仅限本家手牌为 3k+1 未摸牌时杠他人弃牌)
  if (myHand.value.length % 3 === 1 && lastDiscarded.value && lastDiscarded.value.discardBy !== 0) {
    const d = lastDiscarded.value;
    const matches = myHand.value.filter((c) => c.type === d.type && c.value === d.value);
    if (matches.length === 3 && !(isHongzhongMode.value && isHongzhong(d))) {
      gangTile = d;
      resolvedGangType = 'ming';
      const takeThree = matches.slice(0, 3);
      const idsToRemove = new Set(takeThree.map((c) => c.id));
      myHand.value = myHand.value.filter((c) => !idsToRemove.has(c.id));
      meldedTiles.value.push([...takeThree, d]);
      const dIdx = discards.value.findIndex((c) => c.id === d.id);
      if (dIdx >= 0) {
        discards.value.splice(dIdx, 1);
      } else if (discards.value.length > 0 && discards.value[discards.value.length - 1].type === d.type && discards.value[discards.value.length - 1].value === d.value) {
        discards.value.pop();
      }
      kongedOpponentSeat = d.discardBy !== undefined ? d.discardBy : turnPlayer.value;
      lastDiscarded.value = null;
      pendingPengTurn.value = 0;
      turnPlayer.value = 0;
    }
  } else if (turnPlayer.value === 0 && myHand.value.length % 3 === 2) {
    // Case 2: 暗杠 (手牌中有4张完全相同的牌，红中场四红中为直接胡牌，不作为暗杠)
    const counts = new Map<string, MahjongTile[]>();
    myHand.value.forEach((c) => {
      if (isHongzhongMode.value && isHongzhong(c)) return;
      const k = `${c.type}-${c.value}`;
      if (!counts.has(k)) counts.set(k, []);
      counts.get(k)!.push(c);
    });
    let foundAnGang = false;
    for (const tiles of counts.values()) {
      if (tiles.length === 4) {
        gangTile = tiles[0];
        resolvedGangType = 'an';
        const ids = new Set(tiles.map((c) => c.id));
        myHand.value = myHand.value.filter((c) => !ids.has(c.id));
        meldedTiles.value.push(tiles);
        foundAnGang = true;
        break;
      }
    }

    // Case 3: 补杠 (将摸到的第4张牌加入已碰出的3张明刻中)
    if (!foundAnGang) {
      for (const meld of meldedTiles.value) {
        if (meld.length === 3 && meld[0].type === meld[1].type && meld[0].value === meld[1].value) {
          const matchIdx = myHand.value.findIndex((c) => c.type === meld[0].type && c.value === meld[0].value);
          if (matchIdx !== -1) {
            const addedTile = myHand.value.splice(matchIdx, 1)[0];
            gangTile = addedTile;
            resolvedGangType = 'bu';
            meld.push(addedTile);
            break;
          }
        }
      }
    }
  }

  if (!gangTile || !resolvedGangType) return;

  soundFX.playMahjongClack();
  haptics.sealStamp();
  soundFX.speakAction('gang');

  if (isOnlineMultiplayerRoom.value && props.ws && props.ws.readyState === WebSocket.OPEN) {
    pendingOnlineGangBackup = {
      playerKongExtra: [...playerKongExtra.value],
      seatKongScores: [...seatKongScores.value],
      kongRecords: [...kongRecords.value],
    };
  }

  const isAnOrBuGang = resolvedGangType === 'an' || resolvedGangType === 'bu';
  if (kongedOpponentSeat !== null && kongedOpponentSeat > 0 && kongedOpponentSeat <= 3) {
    playerKongExtra.value[kongedOpponentSeat - 1] += 10;
    seatKongScores.value[0] += 10;
    seatKongScores.value[kongedOpponentSeat] -= 10;
    kongRecords.value.push(`您明杠【${SEAT_NAMES[kongedOpponentSeat]}】得10分 (${SEAT_NAMES[kongedOpponentSeat]}多输10分)`);
  }
  if (isAnOrBuGang) {
    for (let i = 0; i < 3; i++) {
      playerKongExtra.value[i] += 10;
      seatKongScores.value[i + 1] -= 10;
    }
    seatKongScores.value[0] += 30;
    kongRecords.value.push(resolvedGangType === 'an' ? '您暗杠各得三家10分 (+30分，三家各多输10分)' : '您补杠各得三家10分 (+30分，三家各多输10分)');
  }

  if (isOnlineMultiplayerRoom.value && props.ws && props.ws.readyState === WebSocket.OPEN) {
    isOnlineGangPending = true;
    armOnlinePendingWatchdog();
    lastOptimisticSuppTileId = null;
    selectedIndex.value = null;
    tipMessage.value = '杠牌成功，正在从牌墙补抓暗张...';
    drawTable();
    sendGameMove('gang', {
      absSeat: mySeat.value,
      tile: gangTile,
      targetDiscardId: resolvedGangType === 'ming' ? gangTile.id : undefined,
      isMingGang: resolvedGangType === 'ming',
      gangType: resolvedGangType,
      fromSeat: kongedOpponentSeat !== null ? relToAbsSeat(kongedOpponentSeat) : undefined,
    });
    saveLocalSnapshot();
    return;
  }

  // 杠后从牌墙末尾补一张牌入座 (单机/试玩本地发牌)
  let suppTile: MahjongTile | null = null;
  if (wallPool.length > 0) {
    suppTile = wallPool.pop()!;
    wallCount.value = wallPool.length;
    myHand.value.push(suppTile);
    myHand.value = sortHand(myHand.value);
    lastOptimisticSuppTileId = suppTile.id;
    lastDrawnTileIdByRelSeat[0] = suppTile.id;
    tipMessage.value = `杠牌成功！补得【${suppTile.text}】，请自摸胡牌或出牌`;
  } else {
    lastOptimisticSuppTileId = null;
    tipMessage.value = '杠牌成功！牌墙已空，请打出一张牌';
  }

  selectedIndex.value = null;
  drawTable();
  sendGameMove('gang', {
    absSeat: mySeat.value,
    tile: gangTile,
    targetDiscardId: resolvedGangType === 'ming' ? gangTile.id : undefined,
    suppTile,
    isMingGang: resolvedGangType === 'ming',
    gangType: resolvedGangType,
    fromSeat: kongedOpponentSeat !== null ? relToAbsSeat(kongedOpponentSeat) : undefined,
  });
  saveLocalSnapshot();

  // 杠后摸到第4张红中直接胡牌结束游戏！
  if (suppTile && isHongzhongMode.value && myHand.value.filter(isHongzhong).length === 4) {
    submitOrTriggerMyHu(true);
    return;
  }

  if (isAutoHost.value) {
    checkAutoHostTurn();
  } else {
    startTurnCountdown();
  }
}

function submitOrTriggerMyHu(isFourHz: boolean) {
  if (hasWon.value || showSettlement.value || isDrawStalemate.value || dealingPhase.value === 'settled') return;
  if (isOnlineMultiplayerRoom.value && props.ws && props.ws.readyState === WebSocket.OPEN) {
    if (isOnlineHuPending) return;
    isOnlineHuPending = true;
    armOnlinePendingWatchdog();
    stopTurnCountdown();
    stopClaimCountdown();
    if (autoHostTimer) {
      clearTimeout(autoHostTimer);
      autoHostTimer = null;
    }
    tipMessage.value = isFourHz
      ? '🀄 摸齐四张红中，正在核验权威抓码结算...'
      : '🀄 正在核验自摸和牌并开启权威奖码...';
    drawTable();
    sendGameMove('hu', {
      winner: mySeat.value,
      isFourHz,
      winningHand: myHand.value,
      winningMelds: meldedTiles.value,
      seatKongScores: seatKongScores.value,
      absSeat: mySeat.value,
    });
    return;
  }
  triggerMahjongWin(0, isFourHz);
}

// 规则：仅自摸胡牌 (不可炮胡)，调用统一结算函数
function handleHu() {
  if (!canHu.value || hasWon.value || showSettlement.value || dealingPhase.value !== 'playing') return;
  const isFourHz = isHongzhongMode.value && myHand.value.filter(isHongzhong).length === 4;
  submitOrTriggerMyHu(isFourHz);
}

let botTimer: any = null;

function runOpponentTurns() {
  passTurnToNext(0);
}

function passTurnToNext(fromRelSeat: number) {
  stopTurnCountdown();
  stopClaimCountdown();
  if (botTimer) clearTimeout(botTimer);
  if (hasWon.value || showSettlement.value || isDrawStalemate.value || dealingPhase.value !== 'playing') return;

  const nextRelSeat = (fromRelSeat + 1) % 4;
  const nextAbsSeat = relToAbsSeat(nextRelSeat);

  if (nextRelSeat === 0) {
    // 轮到本家 (玩家) 回合：立即进入抓牌并开始出牌倒计时
    turnPlayer.value = 0;
    startTurnCountdown();
    handleDraw();
  } else {
    // 轮到其它席位 (真实牌友或电脑 AI)
    turnPlayer.value = nextRelSeat;
    startTurnCountdown();
    drawTable();
    saveLocalSnapshot();

    if (props.isPracticeMode && isSeatBot(nextAbsSeat)) {
      const thinkTime = Math.floor(400 + Math.random() * 400);
      const t = setTimeout(() => {
        if (hasWon.value || showSettlement.value || dealingPhase.value !== 'playing') return;
        botPlay(nextRelSeat, () => {
          passTurnToNext(nextRelSeat);
        });
      }, thinkTime);
      botActionTimers.push(t);
    } else {
      // 🛡️ 联机网络或真实牌友席位：由服务端统一推送对局事件，客户端仅更新显示，等待网络消息！
      if (isSeatDisconnected(nextAbsSeat)) {
        tipMessage.value = `🤖 【${SEAT_NAMES[nextRelSeat]}】 离线托管自动出牌中...`;
      } else if (isSeatAutoManaged(nextAbsSeat)) {
        tipMessage.value = `⚡ 【${SEAT_NAMES[nextRelSeat]}】 托管自动出牌中...`;
      } else {
        tipMessage.value = `轮到 【${SEAT_NAMES[nextRelSeat]}】 出牌...`;
      }
      drawTable();
    }
  }
}

function botPlay(seat: number, next: () => void) {
  if (!props.isPracticeMode) return;
  if (hasWon.value || showSettlement.value || isDrawStalemate.value || dealingPhase.value !== 'playing') return;
  const botIdx = seat - 1; // 0=East, 1=North, 2=West
  const botHand = oppHands.value[botIdx] || [];

  // 1. 明杠与碰牌截击判断：检测上一张打出的牌（规则：全场仅限自摸和牌，没有点炮胡）
  if (lastDiscarded.value && lastDiscarded.value.discardBy !== seat) {
    const d = lastDiscarded.value;
    const matches = botHand.filter(c => c.type === d.type && c.value === d.value);
    const canMeld = !(isHongzhongMode.value && isHongzhong(d));

    // ② 明杠 (手牌持 3 张同牌 + 抓到他人打出牌，牌墙有余牌可补才允许杠)
    if (canMeld && wallPool.length > 0 && matches.length === 3 && Math.random() < 0.95) {
      const fromSeat = typeof d.discardBy === 'number' ? d.discardBy : -1;
      const takeThree = matches.slice(0, 3);
      const rmIds = new Set(takeThree.map(c => c.id));
      const remainingHand = botHand.filter(c => !rmIds.has(c.id));
      oppMelds.value[botIdx].push([...takeThree, d]);
      const gDiscardIdx = discards.value.findIndex((c) => c.id === d.id);
      if (gDiscardIdx >= 0) {
        discards.value.splice(gDiscardIdx, 1);
      } else if (discards.value.length > 0 && discards.value[discards.value.length - 1].type === d.type && discards.value[discards.value.length - 1].value === d.value) {
        discards.value.pop();
      }
      lastDiscarded.value = null;
      if (fromSeat >= 0 && fromSeat < 4 && fromSeat !== seat) {
        seatKongScores.value[seat] += 10;
        seatKongScores.value[fromSeat] -= 10;
        if (fromSeat === 0) {
          kongRecords.value.push(`【${SEAT_NAMES[seat]}】明杠您 (-10分)`);
        }
      }
      soundFX.playMahjongClack();
      soundFX.speakAction('gang', { isFemale: botIdx === 1 });
      tipMessage.value = `🀄 【${SEAT_NAMES[seat]}】大喊：杠！明杠拿下【${d.text}】`;

      // 杠后开花：从牌墙末尾补抓一张牌
      if (wallPool.length > 0) {
        const supp = wallPool.pop() || wallPool.shift()!;
        wallCount.value = wallPool.length;
        remainingHand.push(supp);
      }
      oppHands.value[botIdx] = sortHand(remainingHand);
      drawTable();

      // 检查杠上开花或四红中
      if (isHongzhongMode.value && oppHands.value[botIdx].filter(isHongzhong).length === 4) {
        triggerMahjongWin(seat, true);
        return;
      }
      const canGangHu = isHongzhongMode.value ? checkHongzhongWin(oppHands.value[botIdx]) : checkMahjongWin(oppHands.value[botIdx]);
      if (canGangHu) {
        triggerMahjongWin(seat, false);
        return;
      }

      const t = setTimeout(() => {
        if (hasWon.value || showSettlement.value || dealingPhase.value !== 'playing') return;
        const updatedHand = oppHands.value[botIdx];
        const bestDiscardIdx = findSmartBestDiscard(updatedHand);
        const [discarded] = updatedHand.splice(bestDiscardIdx, 1);
        discarded.discardBy = seat;
        oppHands.value[botIdx] = sortHand(updatedHand);

        discards.value.push(discarded);
        lastDiscarded.value = discarded;
        soundFX.playMahjongClack();
        soundFX.speakVoice(discarded.text);
        tipMessage.value = `🀄 【${SEAT_NAMES[seat]}】杠后打出了【${discarded.text}】`;
        drawTable();
        waitForPlayerClaim(() => {
          next();
        });
      }, 1100);
      botActionTimers.push(t);
      return;
    }

    // ③ 碰牌 (手牌持 2 张同牌，依据听牌与牌型理性推理)
    if (canMeld && matches.length >= 2 && shouldSmartPeng(botHand, d)) {
      const takeTwo = matches.slice(0, 2);
      const rmIds = new Set(takeTwo.map(c => c.id));
      oppHands.value[botIdx] = botHand.filter(c => !rmIds.has(c.id));
      oppMelds.value[botIdx].push([...takeTwo, d]);
      const pDiscardIdx = discards.value.findIndex((c) => c.id === d.id);
      if (pDiscardIdx >= 0) {
        discards.value.splice(pDiscardIdx, 1);
      } else if (discards.value.length > 0 && discards.value[discards.value.length - 1].type === d.type && discards.value[discards.value.length - 1].value === d.value) {
        discards.value.pop();
      }
      lastDiscarded.value = null;
      turnPlayer.value = seat;
      pendingPengTurn.value = seat;
      soundFX.playMahjongClack();
      soundFX.speakAction('peng', { isFemale: botIdx === 1 });
      tipMessage.value = `🀄 【${SEAT_NAMES[seat]}】大喊：碰！拿下了【${d.text}】`;
      drawTable();

      const t = setTimeout(() => {
        if (hasWon.value || showSettlement.value || dealingPhase.value !== 'playing') return;
        pendingPengTurn.value = null;
        const updatedHand = oppHands.value[botIdx];
        const bestDiscardIdx = findSmartBestDiscard(updatedHand);
        const [discarded] = updatedHand.splice(bestDiscardIdx, 1);
        discarded.discardBy = seat;
        oppHands.value[botIdx] = sortHand(updatedHand);

        discards.value.push(discarded);
        lastDiscarded.value = discarded;
        soundFX.playMahjongClack();
        soundFX.speakVoice(discarded.text);
        tipMessage.value = `🀄 【${SEAT_NAMES[seat]}】碰后打出了【${discarded.text}】`;
        drawTable();
        waitForPlayerClaim(() => {
          passTurnToNext(seat);
        });
      }, 1100);
      botActionTimers.push(t);
      return;
    }
  }

  // 2. 摸牌判断：仅在手牌为 3k+1 张（如13张）时从牌墙摸1张牌入座
  if (botHand.length % 3 === 1) {
    if (wallPool.length === 0) {
      triggerDrawStalemate('牌墙已摸空，荒牌流局！', true);
      return;
    }
    const drawn = wallPool.shift()!;
    wallCount.value = wallPool.length;
    botHand.push(drawn);
  }

  // 规则：摸到四张红中直接结束游戏！
  if (isHongzhongMode.value && botHand.filter(isHongzhong).length === 4) {
    triggerMahjongWin(seat, true);
    return;
  }

  // Check if bot completed a winning hand by self-draw (自摸和牌)
  const botCanHu = isHongzhongMode.value ? checkHongzhongWin(botHand) : checkMahjongWin(botHand);
  if (botCanHu) {
    triggerMahjongWin(seat, false);
    return;
  }

  // Check for AnGang (4 identical tiles in hand, skip Hongzhong in Hongzhong mode)
  const counts = new Map<string, MahjongTile[]>();
  botHand.forEach(c => {
    if (isHongzhongMode.value && isHongzhong(c)) return;
    const k = `${c.type}-${c.value}`;
    if (!counts.has(k)) counts.set(k, []);
    counts.get(k)!.push(c);
  });
  let didBotSelfGang = false;
  if (wallPool.length > 0) {
    for (const tiles of counts.values()) {
      if (tiles.length === 4) {
        const ids = new Set(tiles.map(c => c.id));
        const remainingHand = botHand.filter(c => !ids.has(c.id));
        oppMelds.value[botIdx].push(tiles);
        for (let k = 0; k < 4; k++) {
          if (k !== seat) seatKongScores.value[k] -= 10;
        }
        seatKongScores.value[seat] += 30;
        kongRecords.value.push(`【${SEAT_NAMES[seat]}】暗杠各家输10分`);
        soundFX.playMahjongClack();
        soundFX.speakAction('gang', { isFemale: botIdx === 1 });
        if (wallPool.length > 0) {
          const supp = wallPool.pop() || wallPool.shift()!;
          wallCount.value = wallPool.length;
          remainingHand.push(supp);
        }
        oppHands.value[botIdx] = sortHand(remainingHand);
        didBotSelfGang = true;
        if (isHongzhongMode.value && oppHands.value[botIdx].filter(isHongzhong).length === 4) {
          triggerMahjongWin(seat, true);
          return;
        }
        const canGangHu = isHongzhongMode.value ? checkHongzhongWin(oppHands.value[botIdx]) : checkMahjongWin(oppHands.value[botIdx]);
        if (canGangHu) {
          triggerMahjongWin(seat, false);
          return;
        }
        break;
      }
    }
  }

  // Check for BuGang (adding a 4th matching tile to an existing 3-tile peng meld)
  if (!didBotSelfGang && wallPool.length > 0) {
    for (const meld of oppMelds.value[botIdx] || []) {
      if (meld.length === 3 && meld[0].type === meld[1].type && meld[0].value === meld[1].value) {
        const matchIdx = botHand.findIndex((c) => c.type === meld[0].type && c.value === meld[0].value);
        if (matchIdx !== -1) {
          const addedTile = botHand.splice(matchIdx, 1)[0];
          meld.push(addedTile);
          for (let k = 0; k < 4; k++) {
            if (k !== seat) seatKongScores.value[k] -= 10;
          }
          seatKongScores.value[seat] += 30;
          kongRecords.value.push(`【${SEAT_NAMES[seat]}】补杠各家输10分`);
          soundFX.playMahjongClack();
          soundFX.speakAction('gang', { isFemale: botIdx === 1 });
          if (wallPool.length > 0) {
            const supp = wallPool.pop() || wallPool.shift()!;
            wallCount.value = wallPool.length;
            botHand.push(supp);
          }
          oppHands.value[botIdx] = sortHand(botHand);
          if (isHongzhongMode.value && oppHands.value[botIdx].filter(isHongzhong).length === 4) {
            triggerMahjongWin(seat, true);
            return;
          }
          const canGangHu = isHongzhongMode.value ? checkHongzhongWin(oppHands.value[botIdx]) : checkMahjongWin(oppHands.value[botIdx]);
          if (canGangHu) {
            triggerMahjongWin(seat, false);
            return;
          }
          break;
        }
      }
    }
  }

  // 2. Pro-level AI selection using findSmartBestDiscard
  const currentBotHand = oppHands.value[botIdx];
  const bestDiscardIdx = findSmartBestDiscard(currentBotHand);
  const [discarded] = currentBotHand.splice(bestDiscardIdx, 1);
  discarded.discardBy = seat;
  oppHands.value[botIdx] = sortHand(currentBotHand);

  // 3. Discarded tile is placed into river
  discards.value.push(discarded);
  lastDiscarded.value = discarded;
  soundFX.playMahjongClack();
  soundFX.speakVoice(discarded.text);
  tipMessage.value = `${['东席', '北席', '西席'][seat - 1]} 打出了【${discarded.text}】`;
  drawTable();
  saveLocalSnapshot();
  waitForPlayerClaim(() => {
    next();
  });
}

function canPlayerClaimTile(tile: MahjongTile): boolean {
  if (!tile || hasWon.value || isDrawStalemate.value) return false;
  const matches = myHand.value.filter((c) => c.type === tile.type && c.value === tile.value);
  const canPengTile = !(isHongzhongMode.value && isHongzhong(tile));
  const playerCanPeng = canPengTile && matches.length >= 2;
  const playerCanGang = canPengTile && matches.length >= 3;
  return playerCanPeng || playerCanGang;
}

function handleRemoteGameEvent(evtAbsSeat: number, evt: any) {
  if (!evt || evtAbsSeat === mySeat.value) return;

  const relSeat = absToRelSeat(evtAbsSeat); // 1=East, 2=North, 3=West
  const botIdx = relSeat - 1; // 0, 1, 2

  if (evt.action === 'draw') {
    // 🛡️ 碰牌绝对优先于摸牌：
    // 1. 如果本家刚碰牌（turnPlayer === 0 && myHand.length % 3 === 2），本家处于碰后必须打出一张牌状态，
    // 绝不允许其它席位因网络竞态到达的过时摸牌事件夺走回合！
    if (turnPlayer.value === 0 && myHand.value.length % 3 === 2) {
      console.warn('🛡️ [麻将防锁死] 本地玩家已碰牌正在等待出牌，忽略竞争态中其它席位的过时摸牌事件！');
      return;
    }
    // 2. 如果当前有其它席位刚碰牌尚未打出，碰牌席位拥有绝对出牌权，其它席位摸牌一律视为失效过时事件丢弃！
    if (pendingPengTurn.value !== null && pendingPengTurn.value !== relSeat) {
      console.warn('🛡️ [麻将防锁死] 当前有玩家碰牌截击正在等待出牌，忽略竞争态中的过时摸牌事件！');
      return;
    }
    stopClaimCountdown();
    let drawnTile = evt.tile;
    if (wallPool.length > 0) {
      if (drawnTile && wallPool[0]?.id === drawnTile.id) {
        wallPool.shift();
      } else if (!drawnTile) {
        drawnTile = wallPool.shift();
      } else {
        const fIdx = wallPool.findIndex((t) => t.id === drawnTile.id);
        if (fIdx >= 0) wallPool.splice(fIdx, 1);
        else wallPool.shift();
      }
    }
    wallCount.value = typeof evt.wallRemaining === 'number' ? evt.wallRemaining : wallPool.length;
    if (drawnTile) {
      oppHands.value[botIdx].push(drawnTile);
      lastDrawnTileIdByRelSeat[relSeat] = drawnTile.id || null;
    }
    turnPlayer.value = relSeat;
    startTurnCountdown();
    tipMessage.value = `🀄 【${SEAT_NAMES[relSeat]}】 摸了一张牌`;
    drawTable();
    saveLocalSnapshot();
  } else if (evt.action === 'discard') {
    pendingPengTurn.value = null;
    const tile = evt.tile;
    if (!tile) return;

    const oppHand = oppHands.value[botIdx] || [];
    const tileIdx = oppHand.findIndex((c: any) => !c.hidden && c.type === tile.type && c.value === tile.value);
    if (tileIdx !== -1) {
      oppHand.splice(tileIdx, 1);
    } else if (oppHand.length > 0) {
      oppHand.pop();
    }
    tile.discardBy = relSeat;
    discards.value.push(tile);
    lastDiscarded.value = tile;
    pendingPengTurn.value = null;
    if (typeof evt.wallRemaining === 'number') {
      wallCount.value = evt.wallRemaining;
    }
    turnPlayer.value = relSeat;
    startTurnCountdown();

    soundFX.playMahjongClack();
    soundFX.speakVoice(tile.text);
    tipMessage.value = `🀄 【${SEAT_NAMES[relSeat]}】 打出了【${tile.text}】`;
    drawTable();
    saveLocalSnapshot();

    if (isOnlineMultiplayerRoom.value && props.ws && props.ws.readyState === WebSocket.OPEN) {
      isWaitingServerClaimWindow.value = Boolean(evt.hasClaimWindow);
      if (evt.hasClaimWindow && (canPeng.value || canGang.value)) {
        waitForPlayerClaim(() => handlePassClaim());
      } else if (evt.hasClaimWindow) {
        tipMessage.value = '🀄 等待其他牌友确认碰/杠...';
        drawTable();
      }
      return;
    }

    waitForPlayerClaim(() => {
      passTurnToNext(relSeat);
    });
  } else if (evt.action === 'peng') {
    // 🛡️ 1. 回滚竞争态中抢跑误摸的牌（碰牌优先于摸牌）
    // 如果本地玩家手牌已为 3k+2（如摸了牌变成14张），必须回滚撤销摸牌，恢复为13张！
    if (myHand.value.length % 3 === 2) {
      const targetMyId = (evt.canceledDrawSeat === mySeat.value && evt.canceledDrawTile?.id)
        ? evt.canceledDrawTile.id
        : (lastOptimisticDrawId || lastDrawnTileIdByRelSeat[0]);
      const myIdx = targetMyId ? myHand.value.findIndex((c) => c.id === targetMyId) : -1;
      const premature = myIdx >= 0 ? myHand.value.splice(myIdx, 1)[0] : myHand.value.pop();
      lastOptimisticDrawId = null;
      lastDrawnTileIdByRelSeat[0] = null;
      if (premature) {
        if (!wallPool.some((t) => t.id === premature.id)) wallPool.unshift(premature);
        wallCount.value = wallPool.length;
        console.log(`🛡️ [客户端碰牌回滚] 席位【${SEAT_NAMES[relSeat]}】碰牌，本家抢跑摸牌【${premature.text}】已回滚至牌墙`);
      }
      selectedIndex.value = null;
    }
    // 回滚其他对手席位误摸的牌
    for (let b = 0; b < 3; b++) {
      if (b !== botIdx) {
        const oppRel = b + 1;
        const mCount = (oppMelds.value[b] || []).length;
        const expected = 13 - mCount * 3;
        while (oppHands.value[b] && oppHands.value[b].length > expected) {
          const targetOppId = (evt.canceledDrawSeat !== undefined && absToRelSeat(Number(evt.canceledDrawSeat)) === oppRel && evt.canceledDrawTile?.id)
            ? evt.canceledDrawTile.id
            : lastDrawnTileIdByRelSeat[oppRel];
          const oppIdx = targetOppId ? oppHands.value[b].findIndex((c) => c.id === targetOppId) : -1;
          const popped = oppIdx >= 0 ? oppHands.value[b].splice(oppIdx, 1)[0] : oppHands.value[b].pop();
          lastDrawnTileIdByRelSeat[oppRel] = null;
          if (popped) {
            if (!wallPool.some((t) => t.id === popped.id)) wallPool.unshift(popped);
            wallCount.value = wallPool.length;
          }
        }
      }
    }

    const tile = evt.tile || lastDiscarded.value;
    const oppHand = oppHands.value[botIdx] || [];
    const serverMeld = Array.isArray(evt.meldTiles) && evt.meldTiles.length === 3 ? evt.meldTiles : null;
    if (tile) {
      const removedTiles: MahjongTile[] = [];
      const filtered = oppHand.filter((c: any) => {
        if (removedTiles.length < 2 && !c.hidden && c.type === tile.type && c.value === tile.value) {
          removedTiles.push(c);
          return false;
        }
        return true;
      });
      while (removedTiles.length < 2 && filtered.length > 0) {
        filtered.pop();
        removedTiles.push(tile);
      }
      oppHands.value[botIdx] = filtered;
      oppMelds.value[botIdx].push(serverMeld || (removedTiles.length === 2 ? [...removedTiles, tile] : [tile, tile, tile]));
      const dIdx = discards.value.findIndex((c) => c.id === tile.id);
      if (dIdx >= 0) {
        discards.value.splice(dIdx, 1);
      } else if (discards.value.length > 0 && discards.value[discards.value.length - 1].type === tile.type && discards.value[discards.value.length - 1].value === tile.value) {
        discards.value.pop();
      }
      lastDiscarded.value = null;
    }
    stopClaimCountdown();
    isClaiming.value = false;
    isWaitingServerClaimWindow.value = false;
    pendingPengTurn.value = relSeat;
    turnPlayer.value = relSeat;
    if (typeof evt.wallRemaining === 'number') {
      wallCount.value = evt.wallRemaining;
    }
    ensureOpponentHandSize(botIdx);
    startTurnCountdown();
    soundFX.playMahjongClack();
    soundFX.speakAction('peng', { isFemale: botIdx === 1 });
    tipMessage.value = `🀄 【${SEAT_NAMES[relSeat]}】 大喊：碰！`;
    drawTable();
    saveLocalSnapshot();
  } else if (evt.action === 'gang') {
    const oppMeldsList = oppMelds.value[botIdx] || [];
    const tile = evt.tile || lastDiscarded.value;
    let gType: 'ming' | 'an' | 'bu' = 'ming';
    if (evt.gangType === 'bu') {
      gType = 'bu';
    } else if (evt.gangType === 'an') {
      gType = 'an';
    } else if (evt.gangType === 'an_or_bu') {
      const hasPeng = Boolean(tile && oppMeldsList.some((m) => m.length === 3 && m[0].type === tile.type && m[0].value === tile.value));
      gType = hasPeng ? 'bu' : 'an';
    } else if (!evt.isMingGang && evt.fromSeat === undefined && !(lastDiscarded.value && lastDiscarded.value.discardBy !== relSeat)) {
      const hasPeng = Boolean(tile && oppMeldsList.some((m) => m.length === 3 && m[0].type === tile.type && m[0].value === tile.value));
      gType = hasPeng ? 'bu' : 'an';
    }

    // 🛡️ 明杠同样具有截击出牌优先级，回滚非杠牌席位的抢跑摸牌
    if (gType === 'ming' && lastDiscarded.value) {
      if (myHand.value.length % 3 === 2) {
        const targetMyId = (evt.canceledDrawSeat === mySeat.value && evt.canceledDrawTile?.id)
          ? evt.canceledDrawTile.id
          : (lastOptimisticDrawId || lastDrawnTileIdByRelSeat[0]);
        const myIdx = targetMyId ? myHand.value.findIndex((c) => c.id === targetMyId) : -1;
        const premature = myIdx >= 0 ? myHand.value.splice(myIdx, 1)[0] : myHand.value.pop();
        lastOptimisticDrawId = null;
        lastDrawnTileIdByRelSeat[0] = null;
        if (premature) {
          if (!wallPool.some((t) => t.id === premature.id)) wallPool.unshift(premature);
          wallCount.value = wallPool.length;
        }
        selectedIndex.value = null;
      }
      for (let b = 0; b < 3; b++) {
        if (b !== botIdx) {
          const oppRel = b + 1;
          const mCount = (oppMelds.value[b] || []).length;
          const expected = 13 - mCount * 3;
          while (oppHands.value[b] && oppHands.value[b].length > expected) {
            const targetOppId = (evt.canceledDrawSeat !== undefined && absToRelSeat(Number(evt.canceledDrawSeat)) === oppRel && evt.canceledDrawTile?.id)
              ? evt.canceledDrawTile.id
              : lastDrawnTileIdByRelSeat[oppRel];
            const oppIdx = targetOppId ? oppHands.value[b].findIndex((c) => c.id === targetOppId) : -1;
            const popped = oppIdx >= 0 ? oppHands.value[b].splice(oppIdx, 1)[0] : oppHands.value[b].pop();
            lastDrawnTileIdByRelSeat[oppRel] = null;
            if (popped) {
              if (!wallPool.some((t) => t.id === popped.id)) wallPool.unshift(popped);
              wallCount.value = wallPool.length;
            }
          }
        }
      }
    }
    stopClaimCountdown();
    isClaiming.value = false;
    isWaitingServerClaimWindow.value = false;
    pendingPengTurn.value = relSeat;

    const fromRelSeat = evt.fromSeat !== undefined
      ? absToRelSeat(Number(evt.fromSeat))
      : (typeof lastDiscarded.value?.discardBy === 'number' ? lastDiscarded.value.discardBy : -1);

    const oppHand = oppHands.value[botIdx] || [];
    const serverGangMeld = Array.isArray(evt.meldTiles) && evt.meldTiles.length === 4 ? evt.meldTiles : null;
    if (tile) {
      const removeNeed = gType === 'bu' ? 1 : (gType === 'an' ? 4 : 3);
      const removedTiles: MahjongTile[] = [];
      const filtered = oppHand.filter((c: any) => {
        if (removedTiles.length < removeNeed && !c.hidden && c.type === tile.type && c.value === tile.value) {
          removedTiles.push(c);
          return false;
        }
        return true;
      });
      while (removedTiles.length < removeNeed && filtered.length > 0) {
        filtered.pop();
        removedTiles.push(tile);
      }
      oppHands.value[botIdx] = filtered;

      if (gType === 'bu') {
        const existingPeng = oppMeldsList.find((m) => m.length === 3 && m[0].type === tile.type && m[0].value === tile.value);
        if (existingPeng) {
          if (serverGangMeld) {
            existingPeng.splice(0, existingPeng.length, ...serverGangMeld);
          } else {
            existingPeng.push(removedTiles[0] || tile);
          }
        } else {
          oppMelds.value[botIdx].push(serverGangMeld || [tile, tile, tile, tile]);
        }
      } else if (gType === 'an') {
        oppMelds.value[botIdx].push(serverGangMeld || (removedTiles.length === 4 ? removedTiles : [tile, tile, tile, tile]));
      } else {
        oppMelds.value[botIdx].push(serverGangMeld || (removedTiles.length === 3 ? [...removedTiles, tile] : [tile, tile, tile, tile]));
        if (discards.value.length > 0 && discards.value[discards.value.length - 1].id === tile.id) {
          discards.value.pop();
        } else {
          const dIdx = discards.value.findIndex((c) => c.id === tile.id);
          if (dIdx >= 0) discards.value.splice(dIdx, 1);
        }
        lastDiscarded.value = null;
      }
    }

    if (Array.isArray(evt.seatKongScores) && evt.seatKongScores.length === 4) {
      for (let relS = 0; relS < 4; relS++) {
        seatKongScores.value[relS] = evt.seatKongScores[relToAbsSeat(relS)] || 0;
      }
    } else if (gType === 'ming' && fromRelSeat >= 0 && fromRelSeat < 4 && fromRelSeat !== relSeat) {
      seatKongScores.value[relSeat] += 10;
      seatKongScores.value[fromRelSeat] -= 10;
    } else if (gType !== 'ming') {
      for (let k = 0; k < 4; k++) {
        if (k !== relSeat) seatKongScores.value[k] -= 10;
      }
      seatKongScores.value[relSeat] += 30;
    }

    if (Array.isArray(evt.kongHistory)) {
      kongRecords.value = formatKongHistoryRecords(evt.kongHistory);
    } else if (gType === 'ming' && fromRelSeat >= 0 && fromRelSeat < 4 && fromRelSeat !== relSeat) {
      if (fromRelSeat === 0) {
        kongRecords.value.push(`【${SEAT_NAMES[relSeat]}】明杠您 (-10分)`);
      } else {
        kongRecords.value.push(`【${SEAT_NAMES[relSeat]}】明杠【${SEAT_NAMES[fromRelSeat]}】(+10分)`);
      }
    } else if (gType !== 'ming') {
      kongRecords.value.push(`【${SEAT_NAMES[relSeat]}】${gType === 'an' ? '暗杠' : '补杠'}各家输10分 (-10分)`);
    }

    if (wallPool.length > 0 || evt.suppTile) {
      let supp: MahjongTile | undefined;
      if (evt.suppTile) {
        const wIdx = wallPool.findIndex((t) => t.id === evt.suppTile.id);
        supp = wIdx >= 0 ? wallPool.splice(wIdx, 1)[0] : evt.suppTile;
      } else {
        supp = wallPool.pop();
      }
      wallCount.value = typeof evt.wallRemaining === 'number' ? evt.wallRemaining : wallPool.length;
      if (supp) {
        oppHands.value[botIdx].push(supp);
        lastDrawnTileIdByRelSeat[relSeat] = supp.id;
      }
    }
    turnPlayer.value = relSeat;
    ensureOpponentHandSize(botIdx);
    startTurnCountdown();
    soundFX.playMahjongClack();
    soundFX.speakAction('gang', { isFemale: botIdx === 1 });
    tipMessage.value = `🀄 【${SEAT_NAMES[relSeat]}】 大喊：杠！`;
    drawTable();
    saveLocalSnapshot();
  } else if (evt.action === 'hu') {
    if (hasWon.value || showSettlement.value || dealingPhase.value === 'settled') return;
    triggerMahjongWin(relSeat, Boolean(evt.isFourHz), { ...evt, fromServerWs: true });
  } else if (evt.action === 'pass') {
    if (turnPlayer.value === relSeat) {
      passTurnToNext(relSeat);
    }
  }
}

let lastProcessedMoveCount = -1;
let lastProcessedMoveKey = '';

function onWsMessage(ev: MessageEvent) {
  if (props.isPracticeMode) return;
  try {
    const msg = JSON.parse(ev.data);
    if (msg.roomId && props.roomId && msg.roomId !== props.roomId) return;

    if (msg.type === 'game_deal') {
      lastProcessedMoveCount = -1;
      lastProcessedMoveKey = '';
      if (msg.deal) {
        clearLocalSnapshot();
        dealNewMatch(undefined, true, msg.deal);
      }
    } else if (msg.type === 'scene_reconnect_ack') {
      if (msg.failed) return;
      if (msg.room?.id && props.roomId && msg.room.id !== props.roomId) return;
      lastAppliedReconnectStamp = getReconnectStamp(msg);
      lastProcessedMoveCount = -1;
      lastProcessedMoveKey = '';
      handleReconnectSync(msg);
    } else if (msg.type === 'scene_join_ack' || msg.type === 'scene_room_full' || msg.type === 'scene_room_started') {
      lastProcessedMoveCount = -1;
      lastProcessedMoveKey = '';
      if (msg.deal) {
        const dealIdentifier = msg.deal.dealId || msg.deal.createdAt || '';
        if (dealIdentifier && dealIdentifier === lastAppliedDealId.value && (dealingPhase.value === 'playing' || dealingPhase.value === 'settled')) {
          console.log('🛡️ [麻将防重置] 发牌标识一致，保持当前牌桌或结算状态');
          return;
        }
        clearLocalSnapshot();
        dealNewMatch(undefined, false, msg.deal);
      }
    } else if (msg.type === 'scene_room_update') {
      const updatedPlayers = msg.room?.players || msg.players;
      if (Array.isArray(updatedPlayers)) {
        updatedPlayers.forEach((p: any) => {
          if (p && typeof p.seat === 'number') {
            if (p.isAutoManaged || p.isDisconnected) {
              autoManagedSeats.value[p.seat] = true;
            } else {
              delete autoManagedSeats.value[p.seat];
            }
            if (p.isDisconnected) {
              disconnectedSeats.value[p.seat] = true;
            } else {
              delete disconnectedSeats.value[p.seat];
            }
          }
        });
        const selfPl = updatedPlayers.find(
          (p: any) =>
            p &&
            (p.seat === mySeat.value ||
              (props.currentUser?.id && (p.id === props.currentUser.id || p.originalId === props.currentUser.id)))
        );
        if (selfPl && typeof selfPl.isReady === 'boolean') {
          hasClickedReady.value = selfPl.isReady;
        }
        if (msg.settlementExpiresAt && (hasWon.value || isDrawStalemate.value)) {
          startSettlementCountdown(msg.settlementExpiresAt, msg.serverTs);
        }
        drawTable();
      }
    } else if (msg.type === 'set_auto_host') {
      const sSeat = typeof msg.seat === 'number' ? msg.seat : (typeof msg.absSeat === 'number' ? msg.absSeat : -1);
      const isManaged = Boolean(msg.isAutoManaged ?? msg.isAutoHost);
      const isDisc = Boolean(msg.isDisconnected);
      if (sSeat >= 0) {
        if (isManaged) {
          autoManagedSeats.value[sSeat] = true;
        } else {
          delete autoManagedSeats.value[sSeat];
        }
        if (isDisc) {
          disconnectedSeats.value[sSeat] = true;
        } else if (!isManaged) {
          delete disconnectedSeats.value[sSeat];
        }
        if (sSeat === mySeat.value) {
          isAutoHost.value = isManaged;
          if (isAutoHost.value && turnPlayer.value === 0) {
            checkAutoHostTurn();
          }
        }
        drawTable();
      }
    } else if (msg.type === 'game_event') {
      // 深度合并 payload 与 event，确保 tile、absSeat、action、combo 等关键对局字段绝不遗漏
      const evt: any = { ...(msg.payload || {}), ...(msg.event || {}) };
      if (!evt.tile && (msg.payload?.tile || msg.event?.tile)) {
        evt.tile = msg.payload?.tile || msg.event?.tile;
      }
      if (!evt.action && (msg.payload?.action || msg.event?.action)) {
        evt.action = msg.payload?.action || msg.event?.action;
      }
      if (!evt || !evt.action) return;

      if (
        evt.action === 'draw_rejected' ||
        evt.action === 'discard_rejected' ||
        evt.action === 'claim_rejected' ||
        evt.action === 'hu_rejected'
      ) {
        const targetAbs = typeof evt.absSeat === 'number' ? evt.absSeat : (typeof evt.seat === 'number' ? evt.seat : -1);
        if (targetAbs === mySeat.value) {
          clearOnlinePendingFlags();
          isWaitingServerClaimWindow.value = false;
          if (evt.action === 'claim_rejected' && pendingOnlineGangBackup) {
            playerKongExtra.value = [...pendingOnlineGangBackup.playerKongExtra];
            seatKongScores.value = [...pendingOnlineGangBackup.seatKongScores];
            kongRecords.value = [...pendingOnlineGangBackup.kongRecords];
            pendingOnlineGangBackup = null;
          }
          if (autoHostTimer) {
            clearTimeout(autoHostTimer);
            autoHostTimer = null;
          }
          stopTurnCountdown();
          stopClaimCountdown();
          if (evt.action === 'hu_rejected') {
            hasWon.value = false;
            showSettlement.value = false;
            dealingPhase.value = 'playing';
          }
          const bs = msg.boardState;
          if (Array.isArray(evt.seatHand)) {
            myHand.value = sortHand([...evt.seatHand]);
            myHandDealtCount.value = myHand.value.length;
            lastOptimisticDrawId = null;
            lastOptimisticSuppTileId = null;
            lastDrawnTileIdByRelSeat[0] = null;
            selectedIndex.value = null;
          } else if (evt.action === 'draw_rejected' && myHand.value.length % 3 === 2) {
            const drawId = lastOptimisticDrawId || lastDrawnTileIdByRelSeat[0];
            const idx = drawId ? myHand.value.findIndex((c) => c.id === drawId) : -1;
            const rolled = idx >= 0 ? myHand.value.splice(idx, 1)[0] : myHand.value.pop();
            lastOptimisticDrawId = null;
            lastDrawnTileIdByRelSeat[0] = null;
            selectedIndex.value = null;
            if (rolled && !wallPool.some((t) => t.id === rolled.id)) {
              wallPool.unshift(rolled);
            }
          } else if (evt.action === 'discard_rejected' && evt.tile && myHand.value.length % 3 === 1) {
            const dIdx = discards.value.findIndex((c) => c.id === evt.tile.id);
            const restored = dIdx >= 0 ? discards.value.splice(dIdx, 1)[0] : evt.tile;
            if (restored) {
              delete restored.discardBy;
              myHand.value = sortHand([...myHand.value, restored]);
              lastDiscarded.value = discards.value.length > 0 ? discards.value[discards.value.length - 1] : null;
            }
          }

          if (bs?.extraState?.mahjongDiscards && Array.isArray(bs.extraState.mahjongDiscards)) {
            discards.value = bs.extraState.mahjongDiscards.map((d: any) => {
              const absS = typeof d.discardBy === 'number' ? d.discardBy : (typeof d.absSeat === 'number' ? d.absSeat : 0);
              return { ...d, discardBy: absToRelSeat(absS) };
            });
            if (bs.extraState.lastDiscarded) {
              const lAbs = typeof bs.extraState.lastDiscarded.discardBy === 'number' ? bs.extraState.lastDiscarded.discardBy : 0;
              lastDiscarded.value = { ...bs.extraState.lastDiscarded, discardBy: absToRelSeat(lAbs) };
            } else {
              lastDiscarded.value = null;
            }
          }
          if (bs?.extraState?.mahjongMelds && Array.isArray(bs.extraState.mahjongMelds)) {
            const normMelds = (raw: any[]): MahjongTile[][] =>
              Array.isArray(raw)
                ? raw
                    .map((m: any) => (Array.isArray(m) ? m : Array.isArray(m?.tiles) ? m.tiles : m?.tile ? Array.from({ length: m.type === 'gang' ? 4 : 3 }, (_, i) => ({ ...m.tile, id: `${m.tile.id}-m-${i}` })) : []))
                    .filter((arr: MahjongTile[]) => arr.length > 0)
                : [];
            meldedTiles.value = normMelds(bs.extraState.mahjongMelds[mySeat.value] || []);
            for (let r = 1; r < 4; r++) {
              oppMelds.value[r - 1] = normMelds(bs.extraState.mahjongMelds[relToAbsSeat(r)] || []);
            }
          }
          if (Array.isArray(bs?.seatKongScores) && bs.seatKongScores.length === 4) {
            for (let relS = 0; relS < 4; relS++) {
              seatKongScores.value[relS] = bs.seatKongScores[relToAbsSeat(relS)] || 0;
            }
          }
          if (typeof evt.turnSeat === 'number') {
            turnPlayer.value = absToRelSeat(evt.turnSeat);
          } else if (typeof bs?.turnSeat === 'number') {
            turnPlayer.value = absToRelSeat(bs.turnSeat);
          }
          if (evt.pendingPengSeat !== undefined) {
            pendingPengTurn.value = evt.pendingPengSeat !== null ? absToRelSeat(evt.pendingPengSeat) : null;
          } else if (bs?.extraState?.pendingPengSeat !== undefined) {
            pendingPengTurn.value = bs.extraState.pendingPengSeat !== null ? absToRelSeat(bs.extraState.pendingPengSeat) : null;
          }
          if (typeof evt.wallRemaining === 'number') {
            wallCount.value = evt.wallRemaining;
          } else if (typeof bs?.extraState?.wallRemaining === 'number') {
            wallCount.value = bs.extraState.wallRemaining;
          }
          if (turnPlayer.value === 0 && !hasWon.value && !isDrawStalemate.value) {
            startTurnCountdown();
          }
          tipMessage.value = evt.action === 'hu_rejected'
            ? '🛡️ 手牌未成胡，已拦截炸胡并同步牌桌状态'
            : `⚠️ 操作已失效，已同步权威牌桌 (${evt.reason || 'sync'})`;
          drawTable();
          saveLocalSnapshot();
        }
        return;
      }

      if (evt.action === 'draw_stalemate') {
        clearOnlinePendingFlags();
        triggerDrawStalemate(evt.message || evt.msg || '牌墙抓毕，本局荒庄（流局）！', true, evt);
        return;
      }

      if (evt.action === 'claim_resolved') {
        stopClaimCountdown();
        isClaiming.value = false;
        isWaitingServerClaimWindow.value = false;
        if (typeof evt.wallRemaining === 'number') {
          wallCount.value = evt.wallRemaining;
        }
        if (typeof evt.nextTurn === 'number') {
          turnPlayer.value = absToRelSeat(evt.nextTurn);
        }
        if (
          turnPlayer.value === 0 &&
          myHand.value.length % 3 === 1 &&
          !hasWon.value &&
          !showSettlement.value &&
          !isDrawStalemate.value &&
          dealingPhase.value === 'playing'
        ) {
          handleDraw();
        } else if (!hasWon.value && !showSettlement.value && !isDrawStalemate.value) {
          const nextAbs = relToAbsSeat(turnPlayer.value);
          if (isSeatDisconnected(nextAbs)) {
            tipMessage.value = `🤖 【${SEAT_NAMES[turnPlayer.value]}】 离线托管自动出牌中...`;
          } else if (isSeatAutoManaged(nextAbs)) {
            tipMessage.value = `⚡ 【${SEAT_NAMES[turnPlayer.value]}】 托管自动出牌中...`;
          } else {
            tipMessage.value = `轮到 【${SEAT_NAMES[turnPlayer.value]}】 出牌...`;
          }
          startTurnCountdown();
          drawTable();
          saveLocalSnapshot();
        }
        return;
      }

      const moveCount = typeof msg.moveCount === 'number' ? msg.moveCount : (typeof evt.moveCount === 'number' ? evt.moveCount : -1);
      const moveKey = evt.action === 'set_auto_host'
        ? ''
        : `${evt.absSeat}_${evt.action}_${evt.tile?.id || evt.tile?.value || ''}_${evt.suppTile?.id || ''}_${evt.nextTurn ?? ''}_${evt.wallRemaining ?? ''}_${moveCount > 0 ? moveCount : ''}`;

      if (moveKey && moveKey === lastProcessedMoveKey) return;
      if (moveKey) lastProcessedMoveKey = moveKey;
      if (moveCount > 0) lastProcessedMoveCount = moveCount;

      const evtAbsSeat = typeof evt.absSeat === 'number' ? evt.absSeat : (typeof evt.seat === 'number' ? evt.seat : -1);

      if (evt.action === 'set_auto_host') {
        const isManaged = Boolean(evt.isAutoManaged ?? evt.isAutoHost);
        const isDisc = Boolean(evt.isDisconnected);
        if (evtAbsSeat >= 0) {
          if (isManaged) {
            autoManagedSeats.value[evtAbsSeat] = true;
          } else {
            delete autoManagedSeats.value[evtAbsSeat];
          }
          if (isDisc) {
            disconnectedSeats.value[evtAbsSeat] = true;
          } else if (!isManaged) {
            delete disconnectedSeats.value[evtAbsSeat];
          }
        }
        if (evtAbsSeat === mySeat.value) {
          isAutoHost.value = isManaged;
          if (isAutoHost.value && turnPlayer.value === 0) {
            checkAutoHostTurn();
          }
        }
        drawTable();
        return;
      }

      if (evtAbsSeat >= 0 && evtAbsSeat !== mySeat.value) {
        handleRemoteGameEvent(evtAbsSeat, evt);
        if (msg.boardState) {
          syncOpponentsFromBoardState(msg.boardState);
        }
      } else if (evtAbsSeat === mySeat.value) {
        if (evt.action === 'draw') {
          clearOnlinePendingFlags();
          isWaitingServerClaimWindow.value = false;
          stopClaimCountdown();
          if (evt.autoDraw && autoHostTimer) {
            clearTimeout(autoHostTimer);
            autoHostTimer = null;
          }
          if (myHand.value.length % 3 === 2) {
            // Reconcile optimistic local draw against server-authoritative tile
            const targetId = lastOptimisticDrawId || lastDrawnTileIdByRelSeat[0];
            if (evt.tile && targetId && evt.tile.id !== targetId) {
              const optIdx = myHand.value.findIndex((c) => c.id === targetId);
              if (optIdx >= 0) {
                const replaced = myHand.value.splice(optIdx, 1, evt.tile)[0];
                if (replaced && !wallPool.some((t) => t.id === replaced.id)) wallPool.unshift(replaced);
                const wIdx = wallPool.findIndex((t) => t.id === evt.tile.id);
                if (wIdx >= 0) wallPool.splice(wIdx, 1);
                tipMessage.value = `摸到了【${evt.tile.text}】${isHongzhong(evt.tile) && isHongzhongMode.value ? '(红中百搭万能)' : ''}，请自摸胡牌或点击出牌`;
                if (isHongzhongMode.value && myHand.value.filter(isHongzhong).length === 4) {
                  drawTable();
                  submitOrTriggerMyHu(true);
                  return;
                }
              }
            }
            lastOptimisticDrawId = null;
            if (evt.tile?.id) lastDrawnTileIdByRelSeat[0] = evt.tile.id;
            if (typeof evt.wallRemaining === 'number') wallCount.value = evt.wallRemaining;
            drawTable();
            return;
          }
          turnPlayer.value = 0;
          if (myHand.value.length % 3 === 1) {
            let tileToAdd = evt.tile;
            if (wallPool.length > 0) {
              if (tileToAdd && wallPool[0]?.id === tileToAdd.id) {
                wallPool.shift();
              } else if (!tileToAdd) {
                tileToAdd = wallPool.shift();
              } else {
                const fIdx = wallPool.findIndex((t) => t.id === tileToAdd.id);
                if (fIdx >= 0) wallPool.splice(fIdx, 1);
                else wallPool.shift();
              }
            }
            wallCount.value = typeof evt.wallRemaining === 'number' ? evt.wallRemaining : wallPool.length;
            if (tileToAdd) {
              myHand.value.push(tileToAdd);
              lastOptimisticDrawId = null;
              lastDrawnTileIdByRelSeat[0] = tileToAdd.id || null;
              soundFX.playMahjongClack();
              haptics.mahjongClack();
              if (isHongzhongMode.value && myHand.value.filter(isHongzhong).length === 4) {
                drawTable();
                saveLocalSnapshot();
                submitOrTriggerMyHu(true);
                return;
              }
              tipMessage.value = `摸到了【${tileToAdd.text}】${isHongzhong(tileToAdd) && isHongzhongMode.value ? '(红中百搭万能)' : ''}，请自摸胡牌或点击出牌`;
              drawTable();
              saveLocalSnapshot();
              if (isAutoHost.value && !evt.autoDraw) {
                checkAutoHostTurn();
              } else if (!evt.autoDraw) {
                startTurnCountdown();
              }
            }
          }
        } else if (evt.action === 'peng') {
          isWaitingServerClaimWindow.value = false;
          if (typeof evt.wallRemaining === 'number') wallCount.value = evt.wallRemaining;
          pendingPengTurn.value = 0;
          turnPlayer.value = 0;
          drawTable();
        } else if (evt.action === 'gang') {
          clearOnlinePendingFlags();
          pendingOnlineGangBackup = null;
          isWaitingServerClaimWindow.value = false;
          if (Array.isArray(evt.seatKongScores) && evt.seatKongScores.length === 4) {
            for (let relS = 0; relS < 4; relS++) {
              seatKongScores.value[relS] = evt.seatKongScores[relToAbsSeat(relS)] || 0;
            }
          }
          if (Array.isArray(evt.kongHistory)) {
            kongRecords.value = formatKongHistoryRecords(evt.kongHistory);
          }
          if (evt.suppTile) {
            if (lastOptimisticSuppTileId && evt.suppTile.id !== lastOptimisticSuppTileId) {
              const optIdx = myHand.value.findIndex((c) => c.id === lastOptimisticSuppTileId);
              if (optIdx >= 0) {
                const replaced = myHand.value.splice(optIdx, 1, evt.suppTile)[0];
                myHand.value = sortHand(myHand.value);
                if (replaced && !wallPool.some((t) => t.id === replaced.id)) wallPool.push(replaced);
              }
            } else if (!lastOptimisticSuppTileId && !myHand.value.some((c) => c.id === evt.suppTile.id)) {
              myHand.value.push(evt.suppTile);
              myHand.value = sortHand(myHand.value);
              const wIdx = wallPool.findIndex((t) => t.id === evt.suppTile.id);
              if (wIdx >= 0) wallPool.splice(wIdx, 1);
              else if (wallPool.length > 0) wallPool.pop();
            }
            lastDrawnTileIdByRelSeat[0] = evt.suppTile.id || null;
            tipMessage.value = `杠牌成功！补得【${evt.suppTile.text}】，请自摸胡牌或出牌`;
          }
          lastOptimisticSuppTileId = null;
          if (typeof evt.wallRemaining === 'number') wallCount.value = evt.wallRemaining;
          pendingPengTurn.value = 0;
          turnPlayer.value = 0;
          if (isHongzhongMode.value && myHand.value.filter(isHongzhong).length === 4) {
            drawTable();
            saveLocalSnapshot();
            submitOrTriggerMyHu(true);
            return;
          }
          drawTable();
          saveLocalSnapshot();
          if (isAutoHost.value) {
            checkAutoHostTurn();
          } else {
            startTurnCountdown();
          }
        } else if (evt.action === 'hu') {
          clearOnlinePendingFlags();
          if (!hasWon.value && !showSettlement.value && dealingPhase.value !== 'settled') {
            triggerMahjongWin(0, Boolean(evt.isFourHz), { ...evt, fromServerWs: true });
          }
        } else if (evt.autoDiscard) {
          // 服务端超时判定为本家自动打牌：严格校验手牌为 3k+2 再移除，防止与本地托管并发重复弃牌
          if (autoHostTimer) {
            clearTimeout(autoHostTimer);
            autoHostTimer = null;
          }
          pendingPengTurn.value = null;
          stopTurnCountdown();
          if (myHand.value.length % 3 === 2) {
            let disc: MahjongTile | undefined;
            if (evt.tile && myHand.value.length > 0) {
              let idx = myHand.value.findIndex((c) => c.id === evt.tile.id);
              if (idx < 0) idx = myHand.value.findIndex((c) => c.type === evt.tile.type && c.value === evt.tile.value);
              if (idx >= 0) {
                disc = myHand.value.splice(idx, 1)[0];
              }
            }
            if (disc) {
              disc.discardBy = 0;
              discards.value.push(disc);
              lastDiscarded.value = disc;
              myHand.value = sortHand(myHand.value);
            }
          }
          selectedIndex.value = null;
          isAutoHost.value = true;
          if (evt.nextTurn !== undefined) {
            turnPlayer.value = absToRelSeat(evt.nextTurn);
          } else {
            turnPlayer.value = 1;
          }
          drawTable();
          saveLocalSnapshot();
        }
      }
    }
  } catch {}
}

let cleanupMahjongLoader: (() => void) | null = null;

watch(() => props.ws, (newWs, oldWs) => {
  if (oldWs) {
    try { oldWs.removeEventListener('message', onWsMessage); } catch {}
  }
  if (!newWs || newWs !== oldWs) {
    clearOnlinePendingFlags();
  }
  if (newWs) {
    try {
      newWs.removeEventListener('message', onWsMessage);
      newWs.addEventListener('message', onWsMessage);
    } catch {}
    if (!props.isPracticeMode && hasClickedReady.value && (hasWon.value || isDrawStalemate.value || dealingPhase.value === 'settled')) {
      const sendReady = () => {
        try {
          newWs.send(JSON.stringify({
            type: 'scene_player_ready',
            roomId: props.roomId,
            playerId: props.currentUser?.id,
            playerName: props.currentUser?.name,
            gameType: 'mahjong',
            sceneMode: props.sceneMode,
            isReady: true,
          }));
        } catch {}
      };
      if (newWs.readyState === WebSocket.OPEN) {
        sendReady();
      } else {
        newWs.addEventListener('open', sendReady, { once: true });
      }
    }
  }
});

watch(
  () => props.reconnectSnapshot,
  (snap) => {
    applyReconnectSnapshotIfMatching(snap);
  }
);

onMounted(() => {
  preloadMahjongAssets();
  cleanupMahjongLoader = onMahjongAssetsLoaded(() => {
    drawTable();
  });
  if (props.ws) {
    props.ws.addEventListener('message', onWsMessage);
  }
  const restored = tryRestoreLocalSnapshot();
  if (!restored) {
    if (props.initialDeal?.seatCards) {
      dealNewMatch(undefined, false, props.initialDeal);
    } else {
      dealNewMatch();
    }
  }
  applyReconnectSnapshotIfMatching(props.reconnectSnapshot);
  updateDimensions();
  window.addEventListener('resize', updateDimensions);
  if (containerRef.value && typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(() => {
      updateDimensions();
    });
    resizeObserver.observe(containerRef.value);
  }
});

onBeforeUnmount(() => {
  cleanupMahjongLoader?.();
  if (drawRafId !== null) {
    cancelAnimationFrame(drawRafId);
    drawRafId = null;
  }
  if (snapshotTimer) {
    clearTimeout(snapshotTimer);
    snapshotTimer = null;
  }
  stopSettlementCountdown();
  if (props.ws) {
    try {
      props.ws.removeEventListener('message', onWsMessage);
    } catch {}
  }
  window.removeEventListener('resize', updateDimensions);
  resizeObserver?.disconnect();
  cleanAllGameTimers();
});
</script>

<style scoped>
/* Scoped custom styling handled inside sub-components */
</style>
