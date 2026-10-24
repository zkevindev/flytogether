export type Piece = { id: string; label: string; number: number; progress: number; shield?: boolean };
export type MatchStats = { rolls: number; takeoffs: number; arrivals: number; captures: number; props_used: number };
export type Player = { stats: MatchStats; id: string; name: string; color: string; ai: boolean; pieces: Piece[]; props: Record<string, number> };
export type Move = {
  piece_id: string; label: string; from: number; to: number; captured: string[]; blocked?: string[];
  jump: boolean; fly: boolean; launch: boolean; finish: boolean; summary: string;
};
export type GameEvent = { id: string; kind: string; text: string; player_id?: string; piece_id?: string; dice_rolls?: number[] };
export type BoardSpec = {
  id: string; name: string; layout: 'cross' | 'circle'; ring_length: number;
  home_length: number; finish: number; jump_length: number; fly_from: number; fly_to: number;
};
export type Game = {
  id: string; ruleset: string; difficulty: 'easy' | 'normal' | 'hard'; board: BoardSpec; mode: string; demo: boolean; version: number; turn: number;
  current: number; phase: 'roll' | 'move' | 'finished'; dice: number; winner?: string;
  players: Player[]; legal_moves: Move[]; history: GameEvent[]; last_move?: Move;
  props_enabled: boolean; active_prop?: string; dice_rolls: number[];
};
export type VoiceSettings = { wake_enabled: boolean; wake_word: string };
export type Coach = { id: string; game_id: string; version: number; player_id: string; move: Move; reason: string; risk: string; threats: number };
export type Room = { instance_id?: string; saved_at?: string; restored?: boolean; save_error?: boolean; id: string; name: string; status: 'waiting' | 'playing' | 'paused' | 'ended'; seq: number; state: Game; voice: string; voice_settings: VoiceSettings; coach?: Coach; coach_expired?: boolean; network?: { invite: string; seats: { player_id: string; occupied: boolean; ready: boolean; online: boolean; robot?: boolean }[] }; me?: { player_id: string; host: boolean } };
export type Options = { network?: boolean; room_name: string; difficulty: 'easy' | 'normal' | 'hard'; waiting: boolean; names: string[]; board: string; mode: string; ai: boolean; demo: boolean; props: boolean };
export type Config = { persistence_enabled?: boolean; save_retention_hours?: number; voice_configured: boolean; version: string; ruleset: string; boards: BoardSpec[] };

export class APIError extends Error {
  constructor(public status: number, public code: string, message: string) { super(message); }
}
export async function api<T>(path: string, body?: unknown): Promise<T> {
  const response = await fetch(path, {
    method: body === undefined ? 'GET' : 'POST',
    credentials: 'same-origin',
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(12000),
  });
  const value = await response.json();
  if (!response.ok) throw new APIError(response.status, value.code, value.message || '连接暂时没有成功，请再试一次');
  return value as T;
}
export const socketURL = (path: string) => (location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + path;
export const colors = ['coral', 'gold', 'blue', 'jade'];
export const palette: Record<string, string> = { coral: '#de594f', gold: '#cd901f', blue: '#387fc0', jade: '#328760' };
