import { useEffect, useState } from "react";
import type { Room } from "../lib/types";
import { api } from "../lib/types";
export function RoomManagement({
  room,
  connected,
  busy,
  onBusy,
  onRoom,
  onDone,
}: {
  room: Room;
  connected: boolean;
  busy: boolean;
  onBusy: (value: boolean) => void;
  onRoom: (room: Room) => void;
  onDone: () => void;
}) {
  const lobby = room.network!;
  const [selected, setSelected] = useState("");
  const [confirm, setConfirm] = useState<"transfer_host" | "claim_host" | null>(
    null,
  );
  const [error, setError] = useState("");
  const [clock, setClock] = useState(Date.now());
  const [observed, setObserved] = useState({
    at: Date.now(),
    seconds: lobby.host_claim_wait_seconds || 0,
  });
  const active = room.status !== "ended" && room.state.phase !== "finished";
  const host = room.state.players.find((p) => p.id === lobby.host_player_id);
  const target = room.state.players.find((p) => p.id === selected);
  const targetSeat = lobby.seats.find((s) => s.player_id === selected);
  const canTransfer =
    !!room.me?.host &&
    !!targetSeat &&
    targetSeat.occupied &&
    targetSeat.online &&
    !targetSeat.robot &&
    targetSeat.player_id !== room.me.player_id &&
    active;
  const canClaim = !!lobby.can_claim_host && active && !room.me?.host;
  const remaining = Math.max(
    0,
    Math.ceil(observed.seconds - (clock - observed.at) / 1000),
  );
  useEffect(() => {
    setObserved({
      at: Date.now(),
      seconds: lobby.host_claim_wait_seconds || 0,
    });
  }, [room.seq, lobby.host_claim_wait_seconds]);
  useEffect(() => {
    const timer = setInterval(() => setClock(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => {
    setConfirm(null);
    setSelected("");
    setError("");
  }, [room.state.id, lobby.host_player_id]);
  const apply = async () => {
    if (!confirm || !connected || busy) return;
    setError("");
    onBusy(true);
    const body = {
      command_id: crypto.randomUUID(),
      game_id: room.state.id,
      expected_version: room.state.version,
      action: confirm,
      target_player_id: confirm === "transfer_host" ? selected : undefined,
    };
    try {
      let next: Room;
      try {
        next = await api<Room>("/api/rooms/" + room.id + "/commands", body);
      } catch (e) {
        if (e instanceof TypeError)
          next = await api<Room>("/api/rooms/" + room.id + "/commands", body);
        else throw e;
      }
      onRoom(next);
      onDone();
    } catch (e) {
      const fresh = await api<{ room: Room | null }>("/api/session").catch(
        () => null,
      );
      if (fresh?.room && fresh.room.id === room.id) onRoom(fresh.room);
      setError(e instanceof Error ? e.message : "交接没有完成，请重试");
    } finally {
      onBusy(false);
    }
  };
  return (
    <div className="room-management">
      <p className="modal-description">
        当前房主：<strong>{host?.name || "等待同步"}</strong> ·{" "}
        {lobby.host_online ? "在线" : "暂时离线"}
        。房主负责开始、暂停、结束和组织再玩，每位玩家仍只操作自己的飞机。
      </p>
      {room.host_change && (
        <p className="room-handover-note">
          最近交接：{room.host_change.from} → {room.host_change.to}（
          {room.host_change.reason === "reclaimed" ? "离线后接任" : "主动转让"}
          ）
        </p>
      )}
      {!connected && (
        <p role="status" className="rule-note">
          正在恢复房间连接，拿到最新状态后才能交接。
        </p>
      )}
      {room.me?.host ? (
        <>
          <fieldset className="host-candidates">
            <legend>把房主交给谁</legend>
            {lobby.seats.map((seat, i) => {
              const reason =
                seat.player_id === room.me?.player_id
                  ? "当前房主"
                  : seat.robot
                    ? "机器人不能担任房主"
                    : !seat.occupied
                      ? "尚未入座"
                      : !seat.online
                        ? "暂时离线"
                        : "在线，可接任";
              const enabled =
                seat.player_id !== room.me?.player_id &&
                !seat.robot &&
                seat.occupied &&
                seat.online &&
                active;
              return (
                <label
                  key={seat.player_id}
                  className={!enabled ? "unavailable" : ""}
                >
                  <input
                    type="radio"
                    name="next-host"
                    value={seat.player_id}
                    checked={selected === seat.player_id}
                    disabled={!enabled || busy || !connected}
                    onChange={() => {
                      setSelected(seat.player_id);
                      setConfirm(null);
                      setError("");
                    }}
                  />
                  <span>
                    <strong>
                      {seat.occupied
                        ? room.state.players[i].name
                        : "等待朋友入座"}
                    </strong>
                    <small>{reason}</small>
                  </span>
                </label>
              );
            })}
          </fieldset>
          {!lobby.seats.some(
            (s) =>
              s.occupied &&
              s.online &&
              !s.robot &&
              s.player_id !== room.me?.player_id,
          ) && (
            <p className="rule-note">
              需要另一位朋友入座并保持在线，才能转让。
            </p>
          )}
          {!confirm && (
            <button
              className="primary-button"
              disabled={!connected || busy || !canTransfer}
              onClick={() => setConfirm("transfer_host")}
            >
              转让房主
            </button>
          )}
        </>
      ) : (
        <section className="host-reclaim" aria-label="房主接任状态">
          <h3>房主离线时怎么办</h3>
          {lobby.host_online ? (
            <p>
              房主仍在线，可以请{host?.name}
              主动转让给你。关闭其中一个标签页不会让仍在线的房主失去身份。
            </p>
          ) : !active ? (
            <p>本局已结束，不能再接任。可联系房主组织下一局。</p>
          ) : lobby.can_claim_host ? (
            <p>
              房主已持续离线，你可以接任房间管理。原房主回来后仍保留自己的席位。
            </p>
          ) : lobby.host_claim_at ? (
            <p role="status">
              正在等待房主重连，
              {remaining > 0 ? `还需约 ${remaining} 秒` : "正在确认离线状态"}
              。房主重连后会取消本次接任等待。
            </p>
          ) : (
            <p>连接恢复后会从服务器确认等待时间，请稍候。</p>
          )}
          {!confirm && (
            <button
              className="primary-button"
              disabled={!connected || busy || !canClaim}
              onClick={() => setConfirm("claim_host")}
            >
              接任房主
            </button>
          )}
        </section>
      )}
      {confirm && (
        <section className="host-confirm" aria-label="确认房主交接">
          <h3>
            {confirm === "transfer_host"
              ? `确认把房主交给${target?.name || "这位朋友"}？`
              : "确认由你接任房主？"}
          </h3>
          <p>
            {confirm === "transfer_host"
              ? "交接后，对方可以开始、暂停、结束游戏，你会保留原来的玩家席位。"
              : "接任后，你可以暂停、结束或组织再玩。离线玩家的飞机仍由其本人回来操作。"}
            骰子、道具和当前回合都会保留。
          </p>
          <div>
            <button
              className="outline-button"
              disabled={busy}
              onClick={() => {
                setConfirm(null);
                setError("");
              }}
            >
              先不交接
            </button>
            <button
              className="primary-button"
              disabled={
                !connected ||
                busy ||
                (confirm === "transfer_host" ? !canTransfer : !canClaim)
              }
              onClick={() => void apply()}
            >
              {busy ? "正在交接…" : "确认交接房主"}
            </button>
          </div>
        </section>
      )}
      {error && (
        <p role="alert" className="room-management-error">
          {error}
        </p>
      )}
      <p className="rule-note">
        离线接任需要服务器确认持续离线并经过等待期，不会自动转让，也不会替离线玩家走棋。
      </p>
    </div>
  );
}
