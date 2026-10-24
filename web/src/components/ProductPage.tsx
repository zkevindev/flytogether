import { useState } from 'react';
import { AirplaneTilt, ArrowRight, ArrowUpRight, Check, DiceFive, Eye, Lightning, Microphone, ShieldCheck, UsersThree, Bomb, ArrowUUpLeft, Lifebuoy, ChatCircleDots } from '@phosphor-icons/react';
import { FlatBoard } from './FlatBoard';
import type { Game } from '../lib/types';
import './ProductPage.css';

type Preview = 'launch' | 'shield' | 'luck';
const examples = {
  launch: { title: '一句话，让小熊号出发', command: '使用起飞卡，让小熊号起飞', answer: '小熊号已经到起飞位，接下来可以掷骰子了。', label: '看起飞', Icon: AirplaneTilt },
  shield: { title: '小小策略，大大的得意', command: '给小熊号加上护盾', answer: '小熊号有了护盾，可以抵挡一次撞机或攻击。', label: '看护盾', Icon: ShieldCheck },
  luck: { title: '再多一点，好运气', command: '帮我使用幸运骰', answer: '幸运骰会掷两次，选较大的点数。这次示意取 6 点。', label: '看幸运骰', Icon: DiceFive },
};
const props = [
  [AirplaneTilt, '起飞卡', '直接送到起飞位'], [Lightning, '加速卡', '在飞的飞机多走两格'],
  [DiceFive, '幸运骰', '掷两次，取较大值'], [ShieldCheck, '护盾', '保护一架飞机'],
  [Bomb, '炸弹', '把对手送回机库'], [ArrowUUpLeft, '回旋镖', '让对手后退三格'],
  [Lifebuoy, '救援卡', '安全撤回起飞位'], [UsersThree, '团队护航', '为自己的航线飞机加盾'],
] as const;

function previewGame(example: Preview, full: boolean): Game {
  const names = ['小朋友', '爸爸', '妈妈', '爷爷'], colors = ['coral', 'gold', 'blue', 'jade'];
  return {
    id: 'product-illustration', ruleset: 'family-props-v3', difficulty: 'normal', board: { id: 'cross', name: '经典十字', layout: 'cross', ring_length: 52, home_length: 6, finish: 58, jump_length: 4, fly_from: 17, fly_to: 29 },
    mode: full ? 'classic' : 'quick', demo: false, version: 1, turn: 1, current: 0, phase: 'roll', dice: example === 'luck' ? 6 : 0, dice_rolls: example === 'luck' ? [3, 6] : [], props_enabled: true, legal_moves: [], history: [],
    players: names.map((name, i) => ({ id: 'player-' + i, name, color: colors[i], ai: false, stats: { rolls: 0, takeoffs: 0, arrivals: 0, captures: 0, props_used: 0 }, props: {}, pieces: Array.from({ length: full ? 4 : 2 }, (_, j) => ({ id: `p${i}-${j}`, label: ['小熊号', '兔子号', '小鹿号', '小猫号'][j], number: j + 1, progress: i === 0 && j === 0 ? example === 'launch' ? 0 : 12 : i === 1 && j === 0 ? 9 : -1, shield: i === 0 && j === 0 && example === 'shield' })) })),
  };
}

export function ProductPage({ onStart, onHome }: { onStart: () => void; onHome: () => void }) {
  const [example, setExample] = useState<Preview>('launch');
  const [full, setFull] = useState(false);
  const selected = examples[example];
  return <div className="product-page">
    <a className="skip-link" href="#product-main">跳到主要内容</a>
    <header className="product-header">
      <button className="product-brand" onClick={onHome} aria-label="一起飞，返回游戏首页"><span><AirplaneTilt size={25} weight="fill" /></span><strong>一起飞<small>FlyTogether</small></strong></button>
      <nav aria-label="产品导航"><a href="#product-play">一起怎么玩</a><a href="#product-pocket">道具口袋</a><a href="#product-faq">常见问题</a></nav>
      <button className="product-cta small" onClick={onStart}>开始一起飞 <ArrowUpRight size={17} /></button>
    </header>
    <main id="product-main" tabIndex={-1}>
      <section className="product-hero" aria-labelledby="product-title">
        <div className="product-hero-copy">
          <p className="product-eyebrow"><span />给全家人的语音飞行棋</p>
          <h1 id="product-title">说一句「掷骰子」，<br />快乐就<span>起飞。</span></h1>
          <p className="product-lead">和孩子围坐一张棋盘，和远方家人邀请开局。<br className="product-desktop-break" />灵犀陪你聊棋局、用道具，也陪旁观的家人加油。</p>
          <div className="product-hero-actions"><button className="product-cta" onClick={onStart}>创建游戏 <ArrowRight size={20} /></button><a className="product-secondary" href="#product-play">先看看怎么玩 <span>↓</span></a></div>
          <ul className="product-trust"><li><Check />免注册</li><li><Check />2–4 人一起玩</li><li><Check />手机、电脑都能打开</li></ul>
          <div className="product-family-note"><span className="product-avatar first"><AirplaneTilt weight="fill" /></span><span className="product-avatar second"><UsersThree weight="fill" /></span><span className="product-avatar third"><Eye weight="fill" /></span><p>上场、陪玩、加油，<br /><strong>每个人都有自己的位置。</strong></p></div>
        </div>
        <div className="product-flight-desk" aria-label="飞行棋玩法预览">
          <div className="product-desk-head"><span><span className="product-live-dot" />周末家庭机场</span><span>玩法示意</span></div>
          <div className="product-preview-board"><FlatBoard game={previewGame(example, full)} disabled onPiece={() => {}} onRoll={() => {}} /></div>
          <div className="product-flight-label"><AirplaneTilt weight="fill" /><span>小熊号<strong>{example === 'launch' ? '准备好，出发！' : example === 'shield' ? '护盾已就位' : '好运在路上'}</strong></span></div>
          <div className="product-example-tabs" role="group" aria-label="选择玩法示意">{(Object.keys(examples) as Preview[]).map(id => { const { Icon, label } = examples[id]; return <button key={id} aria-pressed={example === id} onClick={() => setExample(id)}><Icon weight={example === id ? 'fill' : 'regular'} />{label}</button>; })}</div>
          <div className="product-voice-slip" aria-live="polite"><span className="product-mic-icon"><Microphone weight="fill" /></span><div><small>你可以这样说</small><p>“{selected.command}”</p><span>{selected.answer}</span></div></div>
          <p className="product-preview-caption">点选看示意 · 不开启麦克风，不影响你的对局</p>
        </div>
      </section>

      <section className="product-play" id="product-play" aria-labelledby="play-title">
        <div className="product-section-heading"><p className="product-eyebrow">不同的距离，一样的热闹</p><h2 id="play-title">今天，怎么一起玩？</h2><p>围坐在一起，或把邀请链接发给想念的人。</p></div>
        <div className="product-play-grid">
          <article className="product-couch"><span className="product-section-icon"><UsersThree weight="duotone" /></span><span className="product-small-label">坐在一起</span><h3>一台设备，<br />轮流做小小飞行员。</h3><p>爸爸掷骰子，孩子挑飞机。一边玩，一边聊。点击就能完成整局，灵犀也随时可以帮忙。</p><div className="product-seat-strip"><span>小朋友</span><ArrowRight /><span>爸爸</span><ArrowRight /><span>妈妈</span></div></article>
          <article className="product-distance"><span className="product-section-icon"><AirplaneTilt weight="duotone" /></span><span className="product-small-label">各自一台</span><h3>相隔很远，<br />也能飞在同一张棋盘。</h3><p>复制邀请链接，填昵称入座。全员准备好，房主就能开局；轮到你时，只操作自己的飞机。</p><ol><li><b>01</b>创建房间</li><li><b>02</b>分享邀请</li><li><b>03</b>一起起飞</li></ol></article>
          <article className="product-spectate"><span className="product-section-icon"><Eye weight="duotone" /></span><div><span className="product-small-label">想看就来</span><h3>还可以，请家人来加油。</h3><p>独立观赛链接不占席位。送一个鼓掌，发一句加油，或只问自己的灵犀：“现在谁领先？”</p></div><span className="product-private-note"><ChatCircleDots />每位观众都有自己的语音助手</span></article>
        </div>
      </section>

      <section className="product-pace" aria-labelledby="pace-title">
        <div><p className="product-eyebrow">把节奏交给今天的心情</p><h2 id="pace-title">玩一小会儿，<br />还是认真飞一局？</h2><p>开局前选好节奏。新手可以先选亲子轻松，<br className="product-desktop-break" />也能邀请灵犀机器人，随时凑齐一桌。</p></div>
        <div className="product-pace-options" role="group" aria-label="了解两种对局节奏">
          <button aria-pressed={!full} onClick={() => setFull(false)}><span className="product-mode-symbol"><AirplaneTilt /></span><span><strong>轻快一局<small>先送达 1 架就赢</small></strong><p>每人 2 架飞机，适合先体验一局。</p></span><span className="product-mode-check">{!full && <Check weight="bold" />}</span></button>
          <button aria-pressed={full} onClick={() => setFull(true)}><span className="product-mode-symbol"><UsersThree /></span><span><strong>完整飞行<small>4 架全部抵达</small></strong><p>多一点策略，多几次反败为胜的机会。</p></span><span className="product-mode-check">{full && <Check weight="bold" />}</span></button>
          <p className="product-mode-note">这里仅切换预览。正式玩法在创建游戏时选择。</p>
        </div>
      </section>

      <section className="product-pocket" id="product-pocket" aria-labelledby="pocket-title">
        <div className="product-section-heading"><p className="product-eyebrow">不只等一个六点</p><h2 id="pocket-title">口袋里，装一点小策略。</h2><p>直接起飞、保护同伴，或是来一场善意的反击。<br />亲子轻松保留辅助道具，经典玩法则不使用道具。</p></div>
        <div className="product-prop-grid">{props.map(([Icon, name, description], i) => <article key={name}><span className={'product-prop-icon tint-' + i}><Icon weight="duotone" /></span><h3>{name}</h3><p>{description}</p></article>)}</div>
      </section>

      <section className="product-faq" id="product-faq" aria-labelledby="faq-title"><div><p className="product-eyebrow">开局之前</p><h2 id="faq-title">你可能还想知道</h2></div><div>
        <details><summary>必须开麦克风才能玩吗？</summary><p>不用。掷骰、走棋、用道具都支持点击。语音对话需要允许麦克风；引导期间保持安静，完成或跳过后才开启。</p></details>
        <details><summary>灵犀是玩家，还是语音助手？</summary><p>两种都可以。语音助手不占席位，帮你了解棋局或执行合法操作；邀请灵犀机器人后，它会占一个席位参与比赛。</p></details>
        <details><summary>玩到一半有事，可以下次继续吗？</summary><p>可以。在原浏览器回到游戏，继续保存的对局。联网时也能明确选择暂离托管，回来后再接管；换设备找回席位尚未支持。</p></details>
        <details><summary>观众可以替选手操作吗？</summary><p>不可以。观众只看棋局、发送互动，与自己的灵犀私聊。观众的问题和回复不会广播给比赛中的选手。</p></details>
      </div></section>
      <section className="product-last-call"><AirplaneTilt weight="duotone" /><div><p className="product-eyebrow">棋盘已经准备好了</p><h2>今晚，谁先抵达？</h2><p>叫上家人，把这一局变成共同的小回忆。</p></div><button className="product-cta" onClick={onStart}>创建第一局 <ArrowRight /></button></section>
    </main>
    <footer className="product-footer"><span>一起飞 · FlyTogether</span><span>有家人在，一起飞才好玩。</span><button onClick={onHome}>返回游戏首页 <ArrowUpRight /></button></footer>
  </div>;
}
