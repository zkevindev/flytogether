import type { Game, GameEvent, Player } from './types';

const palette: Record<string, string> = { coral: '#de594f', gold: '#cd901f', blue: '#387fc0', jade: '#328760' };

export type ReplayCardData = { title: string; winner: string; board: string; mode: string; rows: { name: string; color: string; ai: boolean; rolls: number; takeoffs: number; arrivals: number; captures: number; props: number }[]; highlights: string[]; text: string };

const xml = (value: string) => value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;').replaceAll("'", '&apos;');
const short = (value: string, limit: number) => [...value].length > limit ? [...value].slice(0, limit - 1).join('') + '…' : value;

function highlight(event: GameEvent) {
  if (event.kind === 'prop') return event.text;
  if (event.kind !== 'moved') return '';
  return /起飞|回机场|被撞|撞回|抵达|终点|跳跃|飞行/.test(event.text) ? event.text : '';
}

export function replayCardData(game: Game): ReplayCardData | undefined {
  if (!game.winner || game.demo) return undefined;
  const champion = game.players.find(player => player.id === game.winner);
  const cooperative = game.mode === 'coop' && game.winner === 'team';
  if (!champion && !cooperative) return undefined;
  const rows = game.players.map((player: Player) => ({
    name: player.name, color: palette[player.color] || '#648271', ai: player.ai,
    rolls: player.stats?.rolls || 0, takeoffs: player.stats?.takeoffs || 0,
    arrivals: player.stats?.arrivals || 0, captures: player.stats?.captures || 0,
    props: player.stats?.props_used || 0,
  }));
  const highlights = game.history.map(highlight).filter(Boolean).slice(-4).map(text => short(text, 42));
  const winner = cooperative ? '全队' : champion!.name;
  const mode = game.mode === 'quick' ? '轻快一局' : cooperative ? '星河救援 · 合作挑战' : '完整飞行';
  const text = [cooperative ? '🚀 全队共同完成了星河救援！' : `🏆 ${winner}赢下了这次飞行！`, `${game.board.name} · ${mode}`, ...rows.map(row => `${row.name}：掷骰 ${row.rolls} 次 · 起飞 ${row.takeoffs} 架 · 抵达 ${row.arrivals} 架 · 送回 ${row.captures} 次 · 使用道具 ${row.props} 张`), ...(highlights.length ? ['精彩棋步：', ...highlights.map(item => `• ${item}`)] : [])].join('\n');
  return { title: '这一局，一起飞', winner, board: game.board.name, mode, rows, highlights, text };
}

export function replayCardSVG(game: Game) {
  const data = replayCardData(game);
  if (!data) return '';
  const width = 1000, height = 760;
  const rowMarkup = data.rows.map((row, index) => {
    const y = 330 + index * 58;
    return `<rect x="64" y="${y - 31}" width="872" height="48" rx="15" fill="${index % 2 ? '#fbfaf4' : '#f4f5ed'}"/><circle cx="93" cy="${y - 7}" r="8" fill="${xml(row.color)}"/><text x="116" y="${y}" class="row-name">${xml(short(row.name, 12))}${row.ai ? ' · 机器人' : ''}${data.winner === '全队' || row.name === data.winner ? ' · 救援队员' : ''}</text><text x="425" y="${y}" class="row-stat">掷骰 ${row.rolls}</text><text x="540" y="${y}" class="row-stat">起飞 ${row.takeoffs}</text><text x="655" y="${y}" class="row-stat">抵达 ${row.arrivals}</text><text x="770" y="${y}" class="row-stat">送回 ${row.captures}</text><text x="875" y="${y}" class="row-stat">道具 ${row.props}</text>`;
  }).join('');
  const highlights = data.highlights.map((item, index) => `<text x="84" y="${618 + index * 29}" class="highlight">${index + 1}. ${xml(item)}</text>`).join('');
  const resultLabel = data.winner === '全队' ? '共同完成救援' : '获胜';
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}"><rect width="1000" height="760" rx="36" fill="#f6f5ec"/><circle cx="900" cy="78" r="120" fill="#e6eddf"/><circle cx="930" cy="70" r="70" fill="#d7e4d3"/><path d="M64 74h124" stroke="#c4a66c" stroke-width="4" stroke-linecap="round"/><text x="64" y="128" class="eyebrow">一起飞 · 家庭回顾</text><text x="64" y="202" class="title">${xml(data.title)}</text><text x="64" y="269" class="winner">🏆 ${xml(short(data.winner, 18))} ${resultLabel}</text><text x="64" y="306" class="subtitle">${xml(data.board)}　·　${xml(data.mode)}</text><text x="64" y="356" class="section">这一局的真实记录</text>${rowMarkup}<text x="64" y="590" class="section">棋谱里的精彩瞬间</text>${highlights || '<text x="84" y="620" class="highlight">这局没有记录到特别棋步，快乐已经抵达。</text>'}<text x="64" y="718" class="footer">数字来自本局棋谱 · 一起陪孩子玩一局</text><style>text{font-family:-apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif;fill:#35463d}.eyebrow{font-size:18px;letter-spacing:3px;fill:#72856f}.title{font-size:46px;font-weight:800}.winner{font-size:30px;font-weight:750;fill:#a8503e}.subtitle{font-size:19px;fill:#788477}.section{font-size:18px;font-weight:700;fill:#657b68}.row-name{font-size:18px;font-weight:650}.row-stat{font-size:15px;fill:#617064}.highlight{font-size:16px;fill:#4c5c50}.footer{font-size:14px;fill:#92998d}</style></svg>`;
}

export async function saveReplayCard(game: Game) {
  const svg = replayCardSVG(game);
  if (!svg) throw new Error('只有正常获胜的对局才能生成回顾卡');
  const source = URL.createObjectURL(new Blob([svg], { type: 'image/svg+xml;charset=utf-8' }));
  try {
    const image = new Image();
    image.src = source;
    await image.decode();
    const canvas = document.createElement('canvas');
    canvas.width = 1500; canvas.height = 1140;
    const context = canvas.getContext('2d');
    if (!context) throw new Error('图片暂时无法生成，请改为复制文字');
    context.drawImage(image, 0, 0, canvas.width, canvas.height);
    const png = await new Promise<Blob>((resolve, reject) => canvas.toBlob(blob => blob ? resolve(blob) : reject(new Error('图片保存失败，请重试')), 'image/png'));
    const url = URL.createObjectURL(png);
    const anchor = document.createElement('a');
    anchor.href = url; anchor.download = '一起飞-家庭回顾卡.png'; anchor.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  } finally {
    URL.revokeObjectURL(source);
  }
}

export async function copyReplayText(game: Game) {
  const data = replayCardData(game);
  if (!data) throw new Error('只有正常获胜的对局才能生成回顾卡');
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(data.text);
      return;
    } catch { /* Try the older browser copy path below. */ }
  }
  const field = document.createElement('textarea');
  field.value = data.text;
  field.style.position = 'fixed'; field.style.opacity = '0';
  document.body.append(field); field.select();
  const copied = document.execCommand('copy');
  field.remove();
  if (!copied) throw new Error('复制没有成功，请检查浏览器的剪贴板权限');
}
