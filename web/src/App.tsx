import { useCallback, useEffect, useRef, useState } from 'react';
import { AirplaneTilt, ArrowClockwise, ArrowRight, ArrowUpRight, Check, CheckCircle, DotsThree, GearSix, House, Info, Microphone, Pause, Play, Plus, Stop, Question, Robot, SpeakerHigh, SpeakerSlash, Sparkle, Trophy, UsersThree, Waveform, X } from '@phosphor-icons/react';
import { Board } from './components/Board';
import { DiceRollModal } from './components/DiceRollModal';
import { PropTray } from './components/PropTray';
import { api, APIError, palette, socketURL } from './lib/types';
import type { Config, Options, Piece, Player, Room, VoiceSettings } from './lib/types';
import { VoiceClient } from './lib/voice';

const defaults: Options = { room_name: '周末家庭机场', difficulty: 'normal', waiting: true, names: ['小朋友', '爸爸', '灵矽'], board: 'cross', mode: 'quick', ai: true, demo: false, props: true };
const defaultVoiceSettings: VoiceSettings = { wake_enabled: true, wake_word: '小灵同学' };
const rules = [
  ['六点起飞', '掷到 6，可以把一架飞机从机场移到起飞位；还会再获得一次掷骰机会。'],
  ['同色跳跃', '落在自己的颜色格上会向前跳 4 格；到达星星飞行点，可以继续沿航线飞行。'],
  ['送回机场', '最终落点有其他玩家的飞机，就把它们送回机场。途经的飞机不会受影响。'],
  ['准确抵达', '进入终点需要恰好的点数，超过就选择其他飞机。快速模式先送达一架获胜。'],
  ['道具口袋', '趣味模式每人各有一张起飞卡、加速卡、幸运骰、炸弹和护盾。掷骰前最多用一张；起飞卡允许任意点数起飞，加速卡让在飞的飞机多走两格，幸运骰掷两次取较大值。炸弹送回对方公共航线上的一架飞机，护盾保护自己航线上的一架飞机，抵挡一次炸弹或撞机；机库、起飞位和终点航道不受影响。'],
];

function Dice({ value, rolling = false }: { value: number; rolling?: boolean }) {
  const points: Record<number, number[]> = { 0: [4], 1: [4], 2: [0, 8], 3: [0, 4, 8], 4: [0, 2, 6, 8], 5: [0, 2, 4, 6, 8], 6: [0, 2, 3, 5, 6, 8] };
  return <span className={'dice ' + (rolling ? 'rolling' : '')} aria-label={value ? value + ' 点' : '等待掷骰'}>
    {Array.from({ length: 9 }, (_, i) => <i key={i} className={points[value]?.includes(i) ? 'pip visible' : 'pip'} />)}
  </span>;
}

function Avatar({ player, size = '' }: { player: Player; size?: string }) {
  return <span className={'avatar ' + player.color + ' ' + size}>{player.ai ? <Robot weight="duotone" /> : <AirplaneTilt weight="fill" />}</span>;
}

function Modal({ title, children, onClose }: { title: string; children: React.ReactNode; onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => { ref.current?.showModal(); }, []);
  return <dialog ref={ref} className="modal" onCancel={onClose} onClick={e => { if (e.target === e.currentTarget) onClose(); }}>
    <div className="modal-head"><h2>{title}</h2><button className="icon-button" aria-label="关闭" onClick={onClose}><X /></button></div>{children}
  </dialog>;
}

export default function App() {
  const [room, setRoom] = useState<Room>();
  const [config, setConfig] = useState<Config>();
  const [loading, setLoading] = useState(true);
  const [fatal, setFatal] = useState('');
  const [toast, setToast] = useState('');
  const [busy, setBusy] = useState(false);
  const [pendingAction, setPendingAction] = useState('');
  const [impact, setImpact] = useState<{ text: string; shield: boolean }>();
  const [diceQueue, setDiceQueue] = useState<{ id: string; text: string; values: number[] }[]>([]);
  const rollHistory = useRef<{ game: string; seen: Set<string> }>({ game: '', seen: new Set() });
  const [boardZoom, setBoardZoom] = useState(false);
  const [connected, setConnected] = useState(false);
  const [modal, setModal] = useState<'setup' | 'rules' | 'voice' | 'end' | null>(null);
  const [setupNew, setSetupNew] = useState(false);
  const [voiceOptions, setVoiceOptions] = useState<VoiceSettings>(defaultVoiceSettings);
  const [voiceSaving, setVoiceSaving] = useState(false);
  const [options, setOptions] = useState<Options>(defaults);
  const [voiceState, setVoiceState] = useState('disconnected');
  const [speech, setSpeech] = useState({ text: '', who: '' });
  const [sound, setSound] = useState(() => localStorage.getItem('fly-sound') !== 'off');
  const voice = useRef<VoiceClient | undefined>(undefined);
  const roomRef = useRef<Room | undefined>(undefined);
  const loadingRef = useRef(false);
  const locked = useRef(false);
  const soundRef = useRef<AudioContext | undefined>(undefined);
  roomRef.current = room;

  const accept = useCallback((next: Room) => setRoom(old => old && old.id === next.id && old.seq > next.seq ? old : next), []);
  const load = useCallback(async () => {
    if (loadingRef.current) return;
    loadingRef.current = true;
    setLoading(true); setFatal('');
    try {
      const [configuration, session] = await Promise.all([api<Config>('/api/config'), api<{ room: Room | null }>('/api/session')]);
      setConfig(configuration);
      let next = session.room || await api<Room>('/api/rooms', defaults);
      try {
        const saved = JSON.parse(localStorage.getItem('fly-voice-settings') || 'null');
        if (saved && typeof saved.wake_word === 'string' && typeof saved.wake_enabled === 'boolean' &&
          (saved.wake_word !== next.voice_settings?.wake_word || saved.wake_enabled !== next.voice_settings?.wake_enabled)) {
          next = await api<Room>('/api/rooms/' + next.id + '/voice-settings', { wake_enabled: saved.wake_enabled, wake_word: saved.wake_word });
        }
      } catch { /* An invalid saved preference falls back to the room default. */ }
      accept(next);
    } catch (error) { setFatal(error instanceof Error ? error.message : '暂时无法连接游戏'); }
    finally { setLoading(false); loadingRef.current = false; }
  }, [accept]);
  useEffect(() => { void load(); return () => { voice.current?.disconnect(); void soundRef.current?.close(); }; }, [load]);
  useEffect(() => { if (!toast) return; const id = setTimeout(() => setToast(''), 4500); return () => clearTimeout(id); }, [toast]);
  useEffect(() => {
    if (!room?.id) return;
    let stopped = false, socket: WebSocket, timer: ReturnType<typeof setTimeout>;
    const connect = () => {
      socket = new WebSocket(socketURL('/ws/game?room_id=' + encodeURIComponent(room.id)));
      socket.onopen = () => setConnected(true);
      socket.onmessage = e => { try { const data = JSON.parse(e.data); if (data.type === 'state' && !stopped && roomRef.current?.id === room.id) accept(data.room); } catch {} };
      socket.onclose = () => { setConnected(false); if (!stopped) timer = setTimeout(connect, 1800); };
      socket.onerror = () => socket.close();
    };
    connect();
    return () => { stopped = true; clearTimeout(timer); socket.close(); setConnected(false); };
  }, [room?.id, accept]);

  const state = room?.state;
  const current = state?.players[state.current];
  useEffect(() => {
    if (!state) return;
    if (rollHistory.current.game !== state.id) {
      rollHistory.current = { game: state.id, seen: new Set(state.history.map(e => e.id)) };
      setDiceQueue([]); setImpact(undefined); return;
    }
    const fresh = state.history.filter(e => !rollHistory.current.seen.has(e.id));
    rollHistory.current.seen = new Set(state.history.map(e => e.id));
    const hit = [...fresh].reverse().find(e => e.kind === 'prop' && e.piece_id);
    if (hit) setImpact({ text: hit.text, shield: hit.text.includes('使用了护盾') });
    const rolls = fresh.filter(e => e.kind === 'rolled' && e.dice_rolls?.length).map(e => ({ id: e.id, text: e.text, values: e.dice_rolls! }));
    if (rolls.length) setDiceQueue(queue => [...queue, ...rolls]);
  }, [state]);

  useEffect(() => {
    if (!impact) return;
    const timer = setTimeout(() => setImpact(undefined), 2600);
    return () => clearTimeout(timer);
  }, [impact]);
  const lifecycle = room?.status || 'playing';
  useEffect(() => {
    if (room?.status && room.status !== 'playing') {
      voice.current?.disconnect(); setDiceQueue([]); setImpact(undefined);
    }
  }, [room?.id, room?.status]);
  const voiceBusy = room ? ['listening', 'thinking', 'speaking'].includes(room.voice) : false;
  const canAct = lifecycle === 'playing' && !!state && !!current && !current.ai && !busy && !voiceBusy && !diceQueue.length && state.phase !== 'finished';

  const chime = useCallback(() => {
    if (!sound) return;
    try {
      const audio = soundRef.current ||= new AudioContext(); void audio.resume();
      const oscillator = audio.createOscillator(), gain = audio.createGain();
      oscillator.connect(gain); gain.connect(audio.destination); oscillator.type = 'sine';
      oscillator.frequency.setValueAtTime(660, audio.currentTime); oscillator.frequency.exponentialRampToValueAtTime(880, audio.currentTime + .12);
      gain.gain.setValueAtTime(.045, audio.currentTime); gain.gain.exponentialRampToValueAtTime(.001, audio.currentTime + .22);
      oscillator.start(); oscillator.stop(audio.currentTime + .22);
    } catch {}
  }, [sound]);

  const command = useCallback(async (action: string, pieceID?: string, targetID?: string) => {
    const r = roomRef.current;
    if (!r || locked.current) return;
    if (action === 'pause' || action === 'end') voice.current?.disconnect();
    locked.current = true; setBusy(true); setPendingAction(action);
    const body = { command_id: crypto.randomUUID(), game_id: r.state.id, expected_version: r.state.version, action, piece_id: action === 'move' ? pieceID : action === 'prop' ? targetID : undefined, prop_id: action === 'prop' ? pieceID : undefined };
    const start = Date.now();
    try {
      let next: Room;
      try { next = await api<Room>('/api/rooms/' + r.id + '/commands', body); }
      catch (error) { if (error instanceof TypeError) next = await api<Room>('/api/rooms/' + r.id + '/commands', body); else throw error; }
      await new Promise(resolve => setTimeout(resolve, Math.max(0, 420 - (Date.now() - start))));
      if (roomRef.current?.id === r.id) { accept(next); chime(); }
    } catch (error) {
      setToast(error instanceof Error ? error.message : '这次操作没有完成');
      if (error instanceof APIError && (error.status === 409 || error.code === 'AI_TURN')) {
        const fresh = await api<{ room: Room | null }>('/api/session').catch(() => null);
        if (fresh?.room) accept(fresh.room);
      }
    } finally { locked.current = false; setBusy(false); setPendingAction(''); }
  }, [accept, chime]);

  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.code === 'Space' && !modal && canAct && state?.phase === 'roll' && !(e.target instanceof HTMLInputElement) && !(e.target instanceof HTMLButtonElement)) {
        e.preventDefault(); void command('roll');
      }
    };
    window.addEventListener('keydown', key); return () => window.removeEventListener('keydown', key);
  }, [canAct, modal, state?.phase, command]);

  const onPiece = (piece: Piece, player: number) => {
    if (!state || !current) return;
    if (!canAct) { setToast(current.ai ? '轮到灵矽啦，稍等它走完。' : '请稍等这次操作完成。'); return; }
    if (state.current !== player) { setToast('现在轮到' + current.name + '，请选择自己的飞机。'); return; }
    if (state.phase === 'roll') { setToast('先掷骰子，再选择飞机。'); return; }
    if (!state.legal_moves.some(m => m.piece_id === piece.id)) {
      setToast(piece.progress === -1 ? '机库里的飞机需要掷到六点才能起飞。' : '这架飞机不能使用本次点数，试试高亮的飞机。'); return;
    }
    void command('move', piece.id);
  };

  const openSetup = (newRoom = false) => {
    if (busy || (lifecycle === 'playing' && (voiceBusy || current?.ai))) { setToast('请等这一回合结束后再调整。'); return; }
    setSetupNew(newRoom);
    setOptions({ room_name: newRoom ? '新的家庭机场' : room?.name || defaults.room_name, difficulty: state?.difficulty || 'normal', waiting: true, names: state?.players.map(p => p.name) || defaults.names, board: state?.board.id || 'cross', mode: state?.mode || 'quick', ai: state?.players.some(p => p.ai) || false, demo: false, props: state?.props_enabled ?? true });
    setModal('setup');
  };
  const restart = async (demo = false) => {
    if (!room || locked.current) return;
    locked.current = true; setBusy(true);
    try { voice.current?.disconnect(); accept(await api<Room>(setupNew ? '/api/rooms/new' : '/api/rooms/' + room.id + '/matches', { ...options, waiting: true, demo })); setModal(null); setSpeech({ text: '', who: '' }); chime(); }
    catch (error) { setToast(error instanceof Error ? error.message : '开局失败'); }
    finally { locked.current = false; setBusy(false); }
  };
  const connectVoice = async () => {
    if (!room || voiceState === 'connecting') return;
    voice.current?.disconnect();
    const client = new VoiceClient(room.id, setVoiceState, (text, who) => setSpeech({ text, who }), setToast);
    voice.current = client;
    try { await client.connect(); } catch (error) { setToast(error instanceof Error ? error.message : '语音连接失败'); }
  };

  const saveVoiceSettings = async () => {
    if (!room) return;
    const settings = { ...voiceOptions, wake_word: voiceOptions.wake_word.trim() };
    if ([...settings.wake_word].length < 2 || [...settings.wake_word].length > 16 || !/^[\p{L}\p{N} ]+$/u.test(settings.wake_word)) {
      setToast('唤醒词请使用 2 到 16 个中文、字母或数字'); return;
    }
    setVoiceSaving(true);
    try {
      voice.current?.disconnect();
      const next = await api<Room>('/api/rooms/' + room.id + '/voice-settings', settings);
      accept(next);
      localStorage.setItem('fly-voice-settings', JSON.stringify(next.voice_settings));
      setModal(null); setSpeech({ text: '', who: '' });
      setToast('语音设置已保存，点击开启语音即可使用');
    } catch (error) { setToast(error instanceof Error ? error.message : '语音设置保存失败'); }
    finally { setVoiceSaving(false); }
  };

  if (loading) return <div className="loading-page"><AirplaneTilt className="loading-plane" weight="fill" /><h1>正在准备家庭机场</h1><p>把棋盘铺好，快乐马上起飞。</p></div>;
  if (fatal || !room || !state || !current) return <div className="loading-page"><House size={48} /><h1>机场暂时还没连接上</h1><p>{fatal}</p><button className="primary-button" onClick={() => void load()}>重新连接 <ArrowClockwise /></button></div>;
  const winner = state.players.find(p => p.id === state.winner);
  const voiceSettings = room.voice_settings || defaultVoiceSettings;
  const voiceLabels: Record<string, string> = { disconnected: '尚未连接', connecting: '正在连接', standby: `说“${voiceSettings.wake_word}”唤醒我`, ready: voiceSettings.wake_enabled ? '已唤醒，可以继续说话' : '实时语音已连接，随时可以说话', listening: '正在听你说', thinking: '正在理解', speaking: '灵矽正在说' };
  const effectiveVoice = voiceBusy ? room.voice : voiceState;
  return (
    <div className="app-shell">
      <header className="site-header">
        <a className="brand" href="/" aria-label="一起飞首页"><span className="brand-mark"><AirplaneTilt size={26} weight="fill" /></span><span className="brand-name">一起飞<span>FlyTogether</span></span></a>
        <div className="header-note"><span className={'status-dot ' + (connected ? 'online' : '')} />{connected ? '家庭机场已连接' : '正在重新连接'}</div>
        <div className="header-actions"><button className="outline-button" aria-label="游戏设置" onClick={() => openSetup(false)}><GearSix size={18} /> 游戏设置</button><button className="text-button rules-top" onClick={() => setModal('rules')}><Question size={18} /> 玩法说明</button><a className="github-link" href="https://github.com/zkevindev/flytogether" target="_blank" rel="noreferrer">项目记录 <ArrowUpRight size={15} /></a></div>
      </header>
      <main>
        <h1 className="sr-only">一起飞家庭飞行棋</h1>
        <section className="page-intro">
          <div><p className="eyebrow"><span /> LITTLE FLIGHTS, BIG FAMILY MOMENTS</p><h1>把快乐，飞到一起<span>。</span></h1><p className="intro-copy">和家人围坐一桌，让每一次起飞，都有欢笑作伴。</p></div>
          <div className="session-tools"><span className="mode-tag"><Sparkle weight="fill" size={15} />{state.board.name} · {state.mode === 'quick' ? '轻快一局' : '完整飞行'}</span><button className="outline-button" onClick={() => openSetup(false)}><GearSix size={18} /> 游戏设置</button></div>
        </section>
        <section className="room-toolbar" aria-label="房间与游戏控制">
          <div><span className="room-label">家庭同屏房间</span><h2>{room.name || '家庭机场'}</h2><p>{({waiting:'等待开始',playing: state.phase === 'finished' ? '本局已完成' : '游戏进行中',paused:'游戏已暂停',ended:'本局已结束'})[lifecycle]} · {({easy:'轻松',normal:'普通',hard:'挑战'})[state.difficulty || 'normal']}难度</p></div>
          <div className="room-controls">
            {lifecycle === 'waiting' && <button className="primary-button" disabled={busy} onClick={() => void command('start')}><Play weight="fill" />开始游戏</button>}
            {lifecycle === 'paused' && <button className="primary-button" disabled={busy} onClick={() => void command('resume')}><Play weight="fill" />继续游戏</button>}
            {lifecycle === 'playing' && !winner && <button className="outline-button" disabled={busy} onClick={() => void command('pause')}><Pause weight="fill" />暂停</button>}
            {lifecycle !== 'ended' && <button className="outline-button end-game" disabled={busy} onClick={() => setModal('end')}><Stop weight="fill" />结束游戏</button>}
            {lifecycle === 'ended' && <button className="primary-button" onClick={() => openSetup(false)}>重新准备 <ArrowClockwise /></button>}
            <button className="outline-button" onClick={() => openSetup(true)}><Plus />创建房间</button>
          </div>
        </section>
        {state.demo && <div className="demo-banner"><Info size={18} />当前是预设演示局面，骰子仍为真实随机。<button onClick={() => openSetup(false)}>开始正常对局 <ArrowRight size={15} /></button></div>}
        <div className="game-layout">
          <section className="board-panel">
            <div className="panel-heading"><div><p className="section-kicker">THE FAMILY AIRPORT</p><h2>{state.board.name}棋盘 <span>{state.players.length} 位飞行员</span></h2></div><div className="board-display-tools"><button className="board-zoom-button" aria-pressed={boardZoom} onClick={() => setBoardZoom(v => !v)}>{boardZoom ? '适合屏幕' : '放大棋盘'}</button><button className="icon-button" aria-label={sound ? '关闭游戏音效' : '开启游戏音效'} onClick={() => { setSound(!sound); localStorage.setItem('fly-sound', sound ? 'off' : 'on'); }}>{sound ? <SpeakerHigh /> : <SpeakerSlash />}</button></div></div>
            <div className="board-turnbar"><span style={{ color: palette[current.color] }}><AirplaneTilt weight="fill" />{winner ? `${winner.name}获胜` : `${current.name}的回合`}</span><strong>{lifecycle === 'waiting' ? '先点击开始游戏' : lifecycle === 'paused' ? '棋盘已保留，点击继续游戏' : lifecycle === 'ended' ? '本局已结束，可重新准备' : state.phase === 'move' ? `${state.dice} 点 · 选择高亮飞机` : '点击中央骰子，开始出发'}</strong>{state.props_enabled && !winner && <button className="prop-shortcut" onClick={() => document.getElementById('prop-pocket')?.scrollIntoView({ behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth', block: 'center' })}>道具口袋 · {Object.values(current.props || {}).reduce((a, b) => a + b, 0)}</button>}</div>
            {boardZoom && <p className="board-zoom-hint">已放大，可滑动查看棋盘；也可以在操作区选择飞机。</p>}
            <div className={'board-viewport ' + (boardZoom ? 'is-zoomed' : '')}><div className="board-stage">{impact && <div className={"board-impact " + (impact.shield ? "shield-impact" : "bomb-impact")} role="status"><Sparkle weight="fill" /><strong>{impact.shield ? "护盾启动" : "炸弹出击"}</strong><span>{impact.text}</span></div>}{lifecycle !== 'playing' && <div className="game-status-overlay"><strong>{lifecycle === 'waiting' ? '飞行员准备好了吗？' : lifecycle === 'paused' ? '先休息一下' : '这一趟飞行已结束'}</strong><p>{lifecycle === 'waiting' ? '确认玩家和难度后，点击开始游戏。' : lifecycle === 'paused' ? '棋盘、道具和骰子已保留，随时继续。' : '保留本局记录，重新准备下一次出发。'}</p><button className="primary-button" disabled={busy} onClick={() => lifecycle === 'ended' ? openSetup(false) : void command(lifecycle === 'waiting' ? 'start' : 'resume')}><Play weight="fill" />{lifecycle === 'waiting' ? '开始游戏' : lifecycle === 'paused' ? '继续游戏' : '重新准备'}</button></div>}<Board game={state} onPiece={onPiece} disabled={!canAct} onRoll={() => void command('roll')} rolling={busy && pendingAction === 'roll'} /></div></div>
            <div className="board-caption"><span><i className="mini-star" />{state.board.ring_length} 个航线格 · {state.board.home_length} 格终点跑道 · 飞行 {state.board.fly_from}→{state.board.fly_to}</span><span>{state.mode === 'quick' ? '先送一架到终点，即可获胜' : '把全部飞机送到终点，即可获胜'}</span></div>
            <div className="crew-list" style={{ '--seats': state.players.length } as React.CSSProperties}>
              {state.players.map((player, i) => <div key={player.id} className={'crew-seat ' + (state.current === i && state.phase !== 'finished' ? 'current' : '')} style={{ '--player-color': palette[player.color] } as React.CSSProperties}>
                <Avatar player={player} /><div className="crew-detail"><strong>{player.name}{player.ai && <span className="ai-tag">AI</span>}</strong><span>{player.pieces.filter(p => p.progress === state.board.finish).length}/{state.mode === 'quick' ? '1' : player.pieces.length} 架抵达</span></div>{state.current === i && state.phase !== 'finished' && <span className="seat-turn">本回合</span>}
              </div>)}
            </div>
          </section>
          <aside className="side-column">
            <section className="turn-panel" aria-live="polite">
              <div className="turn-heading"><span className="section-kicker">{winner ? 'A HAPPY LANDING' : 'YOUR NEXT ADVENTURE'}</span><span className="turn-number">第 {state.turn} 回合</span></div>
              <div className="current-player"><Avatar player={winner || current} size="large" /><div><p>{winner ? '这局的飞行冠军' : current.ai ? '灵矽正在规划航线' : '现在轮到'}</p><h2>{winner?.name || current.name}{winner && <Trophy weight="fill" size={24} />}</h2></div></div>
              {lifecycle !== 'playing' ? <div className="lifecycle-tip"><strong>{lifecycle === 'waiting' ? '等待所有飞行员就位' : lifecycle === 'paused' ? '游戏暂停中' : '本局已经结束'}</strong><p>{lifecycle === 'waiting' ? '可先调整席位、棋盘和 AI 难度，再开始游戏。' : lifecycle === 'paused' ? '暂停期间不会掷骰、移动或消耗道具。继续后可重新开启语音。' : '点击重新准备，开启下一局。'}</p></div> : winner ? <div className="winner-message"><span>每一次出发都值得庆祝。</span><button className="primary-button" onClick={() => openSetup(false)}>再一起飞一局 <ArrowRight /></button></div> : <>
                <div className={'dice-area ' + (busy ? 'is-rolling' : '')}><Dice value={state.dice} rolling={busy && pendingAction === 'roll'} /><div><strong>{state.dice ? state.dice + ' 点' : '准备好出发了吗？'}</strong><span>{state.dice_rolls?.length === 2 ? `幸运骰 ${state.dice_rolls.join(' / ')}，取较大值` : current.ai ? '让它想一想，很快就好' : state.phase === 'move' ? '选一架高亮的飞机' : state.dice === 6 ? '六点奖励，可以再掷一次' : '幸运会落在谁的手心'}</span></div></div>
                {state.phase === 'roll' ? <button className="primary-button roll-button" disabled={!canAct} onClick={() => void command('roll')}><span>{current.ai ? '灵矽的回合' : busy ? (pendingAction === 'roll' ? '骰子转呀转…' : '请稍等…') : '掷骰子'}</span>{current.ai ? <Robot size={22} /> : <span className="keyboard-hint">空格 <ArrowRight size={15} /></span>}</button> : <div className="move-options">{state.legal_moves.map(move => <button key={move.piece_id} className="move-option" disabled={!canAct} onClick={() => void command('move', move.piece_id)}><span className="move-icon" style={{ color: palette[current.color] }}><AirplaneTilt weight="fill" /></span><span><strong>{move.label}</strong><small>{move.summary}</small></span><ArrowRight size={17} /></button>)}</div>}
                <div className="turn-tip"><Info size={15} /><span>{state.phase === 'move' ? '也可以直接点击棋盘上高亮的飞机。' : '只有一个合法走法时，飞机会自动出发。'}</span></div>
              </>}
            </section>
            <PropTray key={state.id + ":" + state.version} game={state} status={lifecycle} disabled={!canAct} onUse={(id, target) => void command('prop', id, target)} />
            <section className="voice-panel">
              <div className="voice-heading"><span className="voice-icon"><Waveform size={24} /></span><div><h2>灵矽，陪你一起飞</h2><p>你的家庭游戏搭子</p></div><span className="voice-spark"><Sparkle size={22} weight="duotone" /></span></div>
              <button className="wake-setting" onClick={() => { setVoiceOptions(voiceSettings); setModal('voice'); }} aria-label="编辑语音唤醒设置"><span>{voiceSettings.wake_enabled ? `唤醒词：${voiceSettings.wake_word}` : '自由对话模式'}</span><span>编辑 <GearSix size={13} /></span></button>
              <div className="voice-bubble" aria-live="polite">{speech.text ? <><span className="speech-label">{speech.who === 'you' ? '你刚刚说' : '灵矽'}</span>{speech.text}</> : <><span className="bubble-quote">“</span>{voiceSettings.wake_enabled ? <>先叫我“{voiceSettings.wake_word}”，<br />再说：“掷骰子。”</> : <>你可以问我，<br />哪架飞机能飞得更远？</>}</>}</div>
              <div className="voice-status"><span className={'status-dot ' + (voiceState === 'ready' ? 'online' : '')} />{config?.voice_configured ? voiceLabels[effectiveVoice] || '等待连接' : '语音待配置 · 点击玩法已就绪'}</div>
              {voiceState === 'disconnected' || voiceState === 'connecting' ? <button className="voice-button" disabled={lifecycle !== 'playing' || !!winner || !config?.voice_configured || voiceState === 'connecting'} onClick={() => void connectVoice()}><Microphone size={20} />{lifecycle === 'paused' ? '继续游戏后开启语音' : lifecycle === 'ended' ? '新开一局后开启语音' : lifecycle !== 'playing' ? '开始游戏后开启语音' : voiceState === 'connecting' ? '连接灵矽中…' : config?.voice_configured ? (voiceSettings.wake_enabled ? '开启语音唤醒' : '开启实时语音') : '语音暂未开启'}</button> :
                <button className={'voice-button ' + (effectiveVoice === 'listening' ? 'listening' : '')} onClick={() => voice.current?.disconnect()}><Microphone size={21} />断开实时语音</button>}
              {effectiveVoice === 'speaking' && <button className="stop-speech" onClick={() => voice.current?.stopPlayback()}>停止播报</button>}
              {voiceSettings.wake_enabled && voiceState !== 'disconnected' && voiceState !== 'connecting' && effectiveVoice !== 'standby' && <button className="stop-speech" onClick={() => voice.current?.standby()}>回到待唤醒</button>}
              <div className="voice-examples"><strong>试着跟我说</strong><span>“推荐一个道具”</span><span>“用炸弹攻击爸爸的小熊号”</span><span>“给我的小熊号加护盾”</span></div><p className="voice-footnote">{!config?.voice_configured ? '语音连接完成后，就能开口操作和询问规则。' : voiceSettings.wake_enabled ? '唤醒后可连续对话 30 秒；等待唤醒时，音频也会发送给灵矽识别。' : '连接后可随时插话，试着说：“掷骰子” 或 “走小熊号”'}</p>
            </section>
            <section className="log-panel"><div className="log-heading"><h2>飞行小记</h2><DotsThree size={22} /></div><ol>{state.history.slice(-4).reverse().map((event, i) => <li key={event.id}><span className={'log-dot ' + (i === 0 ? 'latest' : '')} /><p>{event.text}</p></li>)}</ol></section>
          </aside>
        </div>
        <footer className="page-footer"><span><UsersThree size={18} /> 一张棋盘，一家人的好时光。</span><span>FlyTogether · 用声音，拉近一点距离 <AirplaneTilt size={15} /></span></footer>
      </main>
      {diceQueue[0] && <DiceRollModal key={diceQueue[0].id} roll={diceQueue[0]} onClose={() => setDiceQueue(queue => queue.slice(1))} />}
      {toast && <div className="toast" role="status"><Info size={19} /><span>{toast}</span><button aria-label="关闭提示" onClick={() => setToast('')}><X size={16} /></button></div>}
      {modal === 'voice' && <Modal title="给灵矽一个专属称呼" onClose={() => setModal(null)}>
        <form onSubmit={e => { e.preventDefault(); void saveVoiceSettings(); }}>
          <p className="modal-description">网页打开并开启语音后，说出唤醒词就能开始对话。保存设置后，请重新开启语音。</p>
          <label className="toggle-row"><span><strong>语音唤醒</strong><small>关闭后切换为自由对话</small></span><input type="checkbox" checked={voiceOptions.wake_enabled} onChange={e => setVoiceOptions(o => ({ ...o, wake_enabled: e.target.checked }))} /></label>
          <label className="field-label" htmlFor="wake-word">自定义唤醒词</label>
          <input className="wake-word-input" id="wake-word" value={voiceOptions.wake_word} minLength={2} maxLength={16} required onChange={e => setVoiceOptions(o => ({ ...o, wake_word: e.target.value }))} placeholder="例如：小灵同学、飞行助手" />
          <p className="wake-help">使用 2～16 个中文、字母或数字。选一个容易辨认、较少在日常聊天中出现的称呼。</p>
          <div className="wake-preview">试着说：“{voiceOptions.wake_word.trim() || '小灵同学'}，掷骰子。”</div>
          <p className="modal-description">唤醒后可连续对话 30 秒；超时后等待下次唤醒。等待时麦克风持续开启，音频会发送给灵矽用于识别唤醒词。</p>
          <button className="primary-button" type="submit" disabled={voiceSaving}>{voiceSaving ? '保存中…' : '保存语音设置'}<Check /></button>
        </form>
      </Modal>}
      {modal === 'rules' && <Modal title="一起飞，怎么玩？" onClose={() => setModal(null)}><p className="modal-description">这是一起飞的家庭规则。快速模式先送达一架飞机获胜；完整模式需要送达全部飞机。</p><div className="rules-list">{rules.map(([title, text], i) => <div key={title}><span>{String(i + 1).padStart(2, '0')}</span><section><h3>{title}</h3><p>{text}</p></section></div>)}</div><p className="rule-note">机库、起飞位和自己的终点航道不参与撞机。相同位置可以叠放；不使用三连六惩罚或终点反弹。</p><button className="primary-button" onClick={() => setModal(null)}>懂啦，一起出发 <Check /></button></Modal>}
      {modal === 'end' && <Modal title="结束这一局？" onClose={() => setModal(null)}><p className="modal-description">结束后停止当前对局并保留棋盘记录，可以重新准备下一局。</p><button className="primary-button" disabled={busy} onClick={() => { setModal(null); void command('end'); }}>结束游戏 <Stop weight="fill" /></button></Modal>}
      {modal === 'setup' && <Modal title={setupNew ? '创建家庭房间' : '准备下一次出发'} onClose={() => setModal(null)}>
        <form onSubmit={e => { e.preventDefault(); void restart(false); }}>
          <p className="modal-description">同一块屏幕，围坐一起玩。确认后结束当前局并进入准备阶段，点击“开始游戏”再出发。</p>
          <label className="field-label" htmlFor="room-name">房间名称</label><input id="room-name" className="room-name-input" value={options.room_name} maxLength={20} required onChange={e => setOptions(o => ({...o, room_name: e.target.value}))} />
          <label className="field-label">AI 难度</label><div className="difficulty-options">{([{id:'easy',name:'轻松',desc:'随机选子，不使用道具'},{id:'normal',name:'普通',desc:'优先好走法，使用辅助道具'},{id:'hard',name:'挑战',desc:'规划走法，主动攻击与防守'}] as const).map(level => <button key={level.id} type="button" aria-pressed={options.difficulty === level.id} className={options.difficulty === level.id ? 'selected' : ''} onClick={() => setOptions(o => ({...o,difficulty:level.id}))}><strong>{level.name}</strong><span>{level.desc}</span></button>)}</div><p className="wake-help">难度只影响 AI 对手，不改变随机骰子和真人规则；未邀请 AI 时不会生效。</p>
          <label className="field-label">棋盘路线</label><div className="board-options">{(config?.boards || []).map(board => <button type="button" key={board.id} className={options.board === board.id ? 'selected' : ''} onClick={() => setOptions(o => ({ ...o, board: board.id }))}><strong>{board.name}</strong><span>{board.ring_length} 个航线格</span><small>{board.home_length} 格终点跑道</small></button>)}</div>
          <label className="field-label">飞行员席位</label><div className="segmented">{[2, 3, 4].map(count => <button type="button" key={count} className={options.names.length === count ? 'selected' : ''} onClick={() => setOptions(o => ({ ...o, names: Array.from({ length: count }, (_, i) => o.ai && i === count - 1 ? '灵矽' : (o.names[i] && o.names[i] !== '灵矽' ? o.names[i] : ['小朋友', '爸爸', '妈妈', '家人'][i])) }))}>{count} 位</button>)}</div>
          <div className="name-fields">{options.names.map((name, i) => <label key={i}><span className="name-color" style={{ background: palette[['coral', 'gold', 'blue', 'jade'][i]] }} /><span className="sr-only">第 {i + 1} 位飞行员昵称</span><input maxLength={10} required value={name} onChange={e => setOptions(o => ({ ...o, names: o.names.map((v, n) => n === i ? e.target.value : v) }))} />{options.ai && i === options.names.length - 1 && <Robot size={20} />}</label>)}</div>
          <label className="toggle-row"><span><strong>邀请灵矽加入</strong><small>最后一个席位交给 AI 飞行员</small></span><input type="checkbox" checked={options.ai} onChange={e => setOptions(o => ({ ...o, ai: e.target.checked, names: o.names.map((name, i) => i === o.names.length - 1 ? (e.target.checked ? '灵矽' : '家人') : name) }))} /></label>
          <label className="toggle-row"><span><strong>趣味道具模式</strong><small>每人五张道具：起飞、加速、幸运、炸弹、护盾</small></span><input type="checkbox" checked={options.props} onChange={e => setOptions(o => ({ ...o, props: e.target.checked }))} /></label>
          <label className="field-label">这一局的节奏</label><div className="mode-options"><button type="button" className={options.mode === 'quick' ? 'selected' : ''} onClick={() => setOptions(o => ({ ...o, mode: 'quick' }))}><Sparkle /><strong>轻快一局</strong><span>每人 2 架 · 先到 1 架获胜</span></button><button type="button" className={options.mode === 'classic' ? 'selected' : ''} onClick={() => setOptions(o => ({ ...o, mode: 'classic' }))}><AirplaneTilt /><strong>完整飞行</strong><span>每人 4 架 · 全部抵达获胜</span></button></div>
          <button className="primary-button" disabled={busy} type="submit">{busy ? '准备机场中…' : (setupNew ? '创建房间，进入准备' : '保存设置，进入准备')}<ArrowRight /></button>
          <button type="button" disabled={busy} className="demo-link" onClick={() => void restart(true)}>试试预设演示局面 <ArrowUpRight size={14} /></button>
        </form>
      </Modal>}
    </div>
  );
}
