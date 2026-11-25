<template>
  <Teleport to="body">
    <div
      v-if="show"
      :class="isRotated ? 'rules-modal-rotated-full' : 'rules-modal-standard-full'"
      class="select-none p-3.5 sm:p-5 text-[#3C2C1E]"
    >
      <!-- Header -->
      <div class="flex items-center justify-between pb-3 border-b border-[#D8C7B0] shrink-0">
        <div class="flex items-center gap-2.5">
          <span class="text-xl sm:text-2xl p-1.5 rounded-2xl bg-amber-600/20 text-amber-900 border border-amber-600/40">
            {{ activeTab === 'yansan' ? '🎴' : activeTab === 'mahjong' ? '🀄' : '🃏' }}
          </span>
          <div>
            <h3 class="text-base sm:text-xl font-black text-[#3C2C1E] font-serif-title flex items-center gap-2">
              <span v-if="activeTab === 'yansan'">🎴 经典烟三 · 玩法与输赢计分规则</span>
              <span v-else-if="activeTab === 'mahjong'">🀄 麻将 (红中/普通) · 玩法与输赢计分规则</span>
              <span v-else>🃏 十三张 (十三水) · 玩法与输赢计分规则</span>
            </h3>
            <p class="text-[11px] sm:text-xs text-[#6B5744] mt-0.5">
              <span v-if="activeTab === 'yansan'">经典方块4首发 · 黑桃3/黑桃A双煞同盟与计分规则全览</span>
              <span v-else-if="activeTab === 'mahjong'">红中麻将自摸、刮风下雨杠牌、抓码与大满贯规则全览</span>
              <span v-else>十三水全垒打、打枪、特殊牌型免比与道数比牌规则全览</span>
            </p>
          </div>
        </div>
        <div class="flex items-center gap-2">
          <button
            type="button"
            @click="emit('close')"
            class="w-7 h-7 sm:w-8 sm:h-8 rounded-full bg-[#EFE5D3] hover:bg-[#E5D7C2] border border-[#D8C7B0] flex items-center justify-center text-xs sm:text-sm font-bold text-[#3C2C1E] transition-all active:scale-95 cursor-pointer shadow-xs"
          >
            ✕
          </button>
        </div>
      </div>

      <!-- Rules Content Area -->
      <div class="flex-1 min-h-0 overflow-y-auto overflow-x-auto touch-pan-y touch-pan-x pr-1 text-xs sm:text-sm leading-relaxed text-[#3C2C1E] mt-3">
        <!-- 1. 十三张 (十三水) 规则 -->
        <div v-if="activeTab === 'shisanshui'" class="grid grid-cols-1 md:grid-cols-2 gap-3.5">
          <div class="space-y-3.5">
            <div class="p-3.5 rounded-2xl bg-white/80 border border-[#D8C7B0] shadow-xs space-y-1.5">
              <h4 class="font-black text-amber-900 font-serif-title flex items-center gap-1.5 text-sm sm:text-base border-b border-amber-200 pb-1">
                <span>👑 十三张基本玩法说明</span>
              </h4>
              <p>• <b>人数与发牌</b>：四人场（一副 52 张）或八人包厢（两副 104 张），每位玩家分配 13 张手牌。</p>
              <p>• <b>理牌摆法</b>：13 张手牌需分配为<b>头道（3张）</b>、<b>中道（5张）</b>、<b>尾道（5张）</b>三道。</p>
              <p>• <b>倒水相公规则</b>：必须满足<b>尾道 ≥ 中道 ≥ 头道</b>。若违规摆放（如中道大于尾道，或头道大于中道），判定为倒水相公，三道直接判负（每道 -2 水共 -6 水，对手特殊喜水照扣）；若双方均倒水则打和 0 水。</p>
            </div>

            <div class="p-3.5 rounded-2xl bg-white/80 border border-[#D8C7B0] shadow-xs space-y-2">
              <h4 class="font-black text-amber-900 font-serif-title flex items-center gap-1.5 text-sm sm:text-base border-b border-amber-200 pb-1">
                <span>⚔️ 牌型大小、花色与顺子压制规则</span>
              </h4>
              <p>• <b>牌型大小顺序</b>：五同（八人场） > 同花顺 > 铁支（四条） > 俘虏（葫芦） > 同花 > 顺子 > 三条 > 两对 > 对子 > 乌龙（散牌）。</p>
              <p>• <b>花色大小顺序</b>：当牌型点数完全相同时，按花色比较：<b>黑桃 ♠ > 红桃 ♥ > 梅花 ♣ > 方块 ♦</b>。</p>
              <p>• <b>顺子大小顺序</b>：</p>
              <div class="grid grid-cols-1 sm:grid-cols-2 gap-1.5 pl-2 font-mono text-xs">
                <div class="p-2 rounded-xl bg-amber-50 border border-amber-200">
                  <b class="text-amber-900">① 至尊顺（最大）：</b> 10 - J - Q - K - A
                </div>
                <div class="p-2 rounded-xl bg-amber-50 border border-amber-200">
                  <b class="text-amber-900">② 次大顺（第二）：</b> A - 2 - 3 - 4 - 5
                </div>
                <div class="p-2 rounded-xl bg-amber-50 border border-amber-200 col-span-1 sm:col-span-2">
                  <b class="text-amber-900">③ 普通顺：</b> 9-10-J-Q-K > ... > 3-4-5-6-7 > <b>2-3-4-5-6（最小）</b>
                </div>
              </div>
            </div>
          </div>

          <div class="space-y-3.5">
            <div class="p-3.5 rounded-2xl bg-white/80 border border-[#D8C7B0] shadow-xs space-y-2">
              <h4 class="font-black text-amber-900 font-serif-title flex items-center gap-1.5 text-sm sm:text-base border-b border-amber-200 pb-1">
                <span>📊 三道胜出直接计分规则（实时场 1水 = 50分 · 预约专列 1水 = 1分）</span>
              </h4>
              <div class="overflow-x-auto">
                <table class="w-full text-left border-collapse font-sans text-xs">
                  <thead>
                    <tr class="bg-amber-100/80 text-amber-900 font-bold border-b border-amber-300">
                      <th class="p-1.5">道数</th>
                      <th class="p-1.5">胜出牌型</th>
                      <th class="p-1.5">计分水数</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-amber-100">
                    <tr>
                      <td class="p-1.5 font-bold text-amber-900">头道 (3张)</td>
                      <td class="p-1.5">冲三条 (2~A 冲三)</td>
                      <td class="p-1.5 font-bold text-emerald-700 font-mono">+3 水</td>
                    </tr>
                    <tr>
                      <td class="p-1.5 font-bold text-amber-900">头道 (3张)</td>
                      <td class="p-1.5">其他牌型 (对子、乌龙散牌)</td>
                      <td class="p-1.5 font-mono">+1 水</td>
                    </tr>
                    <tr class="bg-amber-50/50">
                      <td class="p-1.5 font-bold text-blue-900">中道 (5张)</td>
                      <td class="p-1.5">同花顺 / 五同 (八人场)</td>
                      <td class="p-1.5 font-bold text-emerald-700 font-mono">+10 水</td>
                    </tr>
                    <tr class="bg-amber-50/50">
                      <td class="p-1.5 font-bold text-blue-900">中道 (5张)</td>
                      <td class="p-1.5">铁支 (四条)</td>
                      <td class="p-1.5 font-bold text-emerald-700 font-mono">+8 水</td>
                    </tr>
                    <tr class="bg-amber-50/50">
                      <td class="p-1.5 font-bold text-blue-900">中道 (5张)</td>
                      <td class="p-1.5">俘虏 (葫芦)</td>
                      <td class="p-1.5 font-bold text-emerald-700 font-mono">+2 水</td>
                    </tr>
                    <tr class="bg-amber-50/50">
                      <td class="p-1.5 font-bold text-blue-900">中道 (5张)</td>
                      <td class="p-1.5">其他牌型 (顺子、同花、两对等)</td>
                      <td class="p-1.5 font-mono">+1 水</td>
                    </tr>
                    <tr>
                      <td class="p-1.5 font-bold text-purple-900">尾道 (5张)</td>
                      <td class="p-1.5">同花顺 / 五同 (八人场)</td>
                      <td class="p-1.5 font-bold text-emerald-700 font-mono">+5 水</td>
                    </tr>
                    <tr>
                      <td class="p-1.5 font-bold text-purple-900">尾道 (5张)</td>
                      <td class="p-1.5">铁支 (四条)</td>
                      <td class="p-1.5 font-bold text-emerald-700 font-mono">+4 水</td>
                    </tr>
                    <tr>
                      <td class="p-1.5 font-bold text-purple-900">尾道 (5张)</td>
                      <td class="p-1.5">其他牌型 (葫芦、顺子、同花等)</td>
                      <td class="p-1.5 font-mono">+1 水</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>

            <div class="p-3.5 rounded-2xl bg-white/80 border border-[#D8C7B0] shadow-xs space-y-2">
              <h4 class="font-black text-amber-900 font-serif-title flex items-center gap-1.5 text-sm sm:text-base border-b border-amber-200 pb-1">
                <span>💥 十三大特殊牌型与打枪全垒打</span>
              </h4>
              <p>• <b>特殊牌型免比通关</b>：至尊青龙（+36水）、一条龙（+26水）、十二皇族（+24水）、三同花顺（+20水）、三铁支（+16水）、凑一色/全大全小（+10水）、六对半/三同花/三顺子（+6水）。</p>
              <p>• <b>打枪（2x）</b>：若玩家头中尾三道全胜某一对手，触发“打枪”，基础三道得分<b>翻倍 (2x)</b>。</p>
              <p>• <b>全垒打（2x）</b>：若玩家打枪全场所有对手，触发“全垒打”，总得分<b>再翻倍 (2x)</b>！</p>
            </div>
          </div>
        </div>

        <!-- 2. 烟三 (三打一 / 双煞) 规则 -->
        <div v-else-if="activeTab === 'yansan'" class="grid grid-cols-1 md:grid-cols-2 gap-3.5">
          <div class="p-3.5 rounded-2xl bg-white/80 border border-[#D8C7B0] shadow-xs space-y-2">
            <h4 class="font-black text-amber-900 font-serif-title flex items-center gap-1.5 text-sm sm:text-base border-b border-amber-200 pb-1">
              <span>🎴 经典烟三 (方块4首发 / 煞牌阵营) 玩法规则</span>
            </h4>
            <p>• <b>发牌与首发</b>：4名玩家各发 13 张牌。拿到<b>方块 4 (♦4)</b> 的玩家拥有首发权，出牌组合必须包含 ♦4。</p>
            <p>• <b>煞牌同盟机制</b>：</p>
            <div class="pl-2 space-y-1.5 text-xs">
              <p>🔹 <b>双人同盟 (2v2)</b>：抓到 <b>黑桃 3 (♠3)</b> 与 <b>黑桃 A (♠A)</b> 的两名玩家自动结为同盟，另外两名玩家结为另一同盟。</p>
              <p>🔹 <b>独煞单挑 (1v3)</b>：若同一个玩家<b>同时抓到 ♠3 与 ♠A</b>，则该玩家单独为一方（双煞独挑），独战其余 3 名玩家！</p>
              <p>🔹 <b>强攻单挑 (1v3)</b>：若玩家手牌极其强劲，可在首轮选择“强攻”，放弃同盟直接一人独战其余 3 家。</p>
            </div>
            <p>• <b>牌型与压牌</b>：支持单张、对子、三条及五张牌型（同花顺 > 四带一 > 俘虏/三带一对 > 同花 > 顺子；无连对与纯炸弹，杂色 10-J-Q-K-A 不可出）。点数 3 最大、4 最小，花色 ♠ > ♥ > ♣ > ♦；后家须用同张数且更大牌型压制，也可自由过牌。</p>
          </div>

          <div class="p-3.5 rounded-2xl bg-white/80 border border-[#D8C7B0] shadow-xs space-y-2">
            <h4 class="font-black text-amber-900 font-serif-title flex items-center gap-1.5 text-sm sm:text-base border-b border-amber-200 pb-1">
              <span>🪙 烟三输赢计分规则（以 5-10 分场为例，10-20 / 20-40 倍率分别 ×2 / ×4）</span>
            </h4>
            <p>• <b>双人同盟 2v2 计分规则</b>：</p>
            <div class="pl-2 space-y-1.5 text-xs">
              <p>🥇 <b>双胜（包揽第 1 与第 2 名出完）</b>：胜利同盟两人各 <b>+10 分</b>，失败同盟两人各 <b>-10 分</b>。</p>
              <p>⚖️ <b>单胜 / 打和</b>：获第 1 名与第 3 名为单胜，胜方各 <b>+5 分</b>、负方各 <b>-5 分</b>；若一方拿第 1 名而另一方包揽第 2、3 名则判定平局打和（各 <b>0 分</b>）。</p>
            </div>
            <p>• <b>独煞 1v3 / 强攻 / 投降计分规则</b>：</p>
            <div class="pl-2 space-y-1.5 text-xs">
              <p>🏆 <b>独煞常规 1v3</b>：独煞第 1 名全赢（<b>+30 分</b>，三家各 -10）、第 2 名半赢（<b>+15 分</b>，各 -5）、第 3 名半输（<b>-15 分</b>，各 +5）、第 4 名全输（<b>-30 分</b>，各 +10）。</p>
              <p>🔥 <b>起手强攻 / 投降</b>：13 张手牌未出且未过牌时，双煞可开启强攻（独赢/独输翻倍 <b>±60 分</b>，三家各 ∓20 分）或起手投降认输（5-10 场 <b>-15 分</b>，三家各 +5 分）。</p>
            </div>
          </div>
        </div>

        <!-- 3. 麻将玩法与规则 -->
        <div v-else class="grid grid-cols-1 md:grid-cols-2 gap-3.5">
          <div class="p-3.5 rounded-2xl bg-white/80 border border-[#D8C7B0] shadow-xs space-y-2">
            <h4 class="font-black text-amber-900 font-serif-title flex items-center gap-1.5 text-sm sm:text-base border-b border-amber-200 pb-1">
              <span>🀄 麻将 (红中/普通) 玩法与规则说明</span>
            </h4>
            <p>• <b>人数与牌数</b>：4 人麻将竞技对局，共 136 张牌（万、条、筒共 108 张，东南西北中发白字牌共 28 张）；红中场中红中（4 张）作百搭万能赖子牌。分为 6 码场与 10 码场。</p>
            <p>• <b>基本流程</b>：打色子定庄开门，庄家起手 14 张，闲家 13 张。依次摸牌、打牌，支持碰牌、杠牌（暗杠/明杠/补杠）与自摸胡牌（仅支持自摸和牌）。</p>
            <p>• <b>刮风下雨（杠牌加分）</b>：发生杠牌时即时结算杠牌分（下雨暗杠/补杠收其余三家各10分共+30分、刮风明杠收点杠方10分）。</p>
            <p>• <b>红中抓码</b>：自摸胡牌后自动从牌墙翻开奖码（6 码场翻 6 码，10 码场翻 10 码；若红中场胡牌手中无红中额外加抓 2 码），翻出“一、五、九、东风、红中”即为有效中码。</p>
            <p>• <b>四张红中天降</b>：若起手或摸齐 4 张红中，直接触发“四红中大满贯”全中奖码通关！</p>
          </div>

          <div class="p-3.5 rounded-2xl bg-white/80 border border-[#D8C7B0] shadow-xs space-y-2">
            <h4 class="font-black text-amber-900 font-serif-title flex items-center gap-1.5 text-sm sm:text-base border-b border-amber-200 pb-1">
              <span>🪙 麻将输赢计分规则</span>
            </h4>
            <p>• <b>结算公式</b>：输赢得分 = <b>底分 (10分) + 命中码数 × 10分 + 杠牌得分</b>。</p>
            <p>• <b>简单计分</b>：无复杂的番数与番型计算，只要自摸胡牌即拿底分，中码越多得分越高，直观清晰！</p>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
defineProps<{
  show: boolean;
  activeTab: 'yansan' | 'mahjong' | 'shisanshui' | string;
  isRotated: boolean;
}>();

const emit = defineEmits<{
  (e: 'close'): void;
}>();
</script>

<style scoped>
.rules-modal-rotated-full {
  position: fixed;
  top: 0;
  left: 0;
  width: 100dvh;
  height: 100dvw;
  transform-origin: 0 0;
  transform: rotate(90deg) translateY(-100%);
  z-index: 120;
  background-color: #FAF4E8;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  box-sizing: border-box;
}

.rules-modal-standard-full {
  position: fixed;
  top: 0;
  left: 0;
  width: 100%;
  height: 100%;
  z-index: 120;
  background-color: #FAF4E8;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  box-sizing: border-box;
}
</style>
