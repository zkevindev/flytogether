import { FlatBoard } from './FlatBoard';
import { BoardFrame, type BoardProps } from './BoardFrame';
export function LightBoard(props: BoardProps) {
  return <BoardFrame {...props}><div className="light-board" data-renderer="flat"><FlatBoard {...props} /></div></BoardFrame>;
}
