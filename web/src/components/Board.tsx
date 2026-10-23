import type { Game, Move, Piece } from '../lib/types';
import { palette } from '../lib/types';
import { boardPieces, createBoardLayout } from '../lib/board-layout';
import { FlightScene } from './FlightScene';

export function Board({ game, onPiece, disabled, onRoll, rolling = false, preview }: {
  game: Game; onPiece: (piece: Piece, player: number) => void; disabled: boolean; onRoll: () => void; rolling?: boolean; preview?: Move;
}) {
  const layout = createBoardLayout(game.board);
  const canRoll = !disabled && game.phase === 'roll';
  return <div className="flight-board" data-game-id={game.id} data-version={game.version}>
    <FlightScene game={game} disabled={disabled} rolling={rolling} preview={preview} onPiece={onPiece} onRoll={onRoll} />
    <div className="plane-legend" aria-label="飞机编号对照">
      {game.players.map(player => <div key={player.id} style={{ color: palette[player.color] }}>
        <strong>{player.name}</strong>
        <span>{player.pieces.map(piece => <span key={piece.id}><b>{piece.number}</b>{piece.label}</span>)}</span>
      </div>)}
    </div>
    <div className="board-access">
      <button type="button" disabled={!canRoll} onClick={onRoll}>{canRoll ? '掷骰子' : `本次骰子 ${game.dice || '未掷'} 点`}</button>
      {boardPieces(game, layout).map(({ player, playerIndex, piece }) => {
        const legal = !disabled && game.current === playerIndex && game.legal_moves.some(move => move.piece_id === piece.id);
        const finished = piece.progress === game.board.finish;
        const status = finished ? '已抵达' : piece.progress < 0 ? '在机库' : piece.progress === 0 ? '在起飞位' : `前进到第 ${piece.progress} 格`;
        return <button type="button" key={piece.id} disabled={!legal} onClick={() => onPiece(piece, playerIndex)}>
          {`${player.name}的${piece.label}，${status}${piece.shield ? '，护盾保护中' : ''}${legal ? '，可以移动' : ''}`}
        </button>;
      })}
    </div>
  </div>;
}
