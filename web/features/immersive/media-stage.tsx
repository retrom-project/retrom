"use client";

import Image from "next/image";
import { useEffect, useRef, useState } from "react";
import type { ImmersiveGame } from "./api";
import styles from "./immersive.module.css";

function useReducedMotion() {
  const [reduced, setReduced] = useState(false);
  useEffect(() => {
    const query = window.matchMedia("(prefers-reduced-motion: reduce)");
    const update = () => setReduced(query.matches);
    update();
    query.addEventListener("change", update);
    return () => query.removeEventListener("change", update);
  }, []);
  return reduced;
}

function Poster({ game, placeholder = false }: { game: ImmersiveGame; placeholder?: boolean }) {
  if (game.coverUrl && !placeholder) {
    return <div className={styles.poster} data-immersive-poster="true"><Image src={game.coverUrl} alt={`${game.title} 封面`} fill sizes="30vw" unoptimized /></div>;
  }
  return <div className={`${styles.poster} ${styles.mediaPlaceholder}`} data-immersive-poster="true" role="img" aria-label={`${game.title} 暂无封面`}>
    <span>RETROM</span><strong>{game.title}</strong><small>{game.defaultCore.name}</small>
  </div>;
}

export function MediaStage({ game }: { game: ImmersiveGame }) {
  const stageRef = useRef<HTMLDivElement>(null);
  const stalledTimer = useRef<number | null>(null);
  const [inView, setInView] = useState(true);
  const [pageVisible, setPageVisible] = useState(true);
  const [readyGameId, setReadyGameId] = useState<string | null>(null);
  const [videoFailed, setVideoFailed] = useState(false);
  const [videoPlaying, setVideoPlaying] = useState(false);
  const reducedMotion = useReducedMotion();
  const validVideoUrl = game.videoUrl?.startsWith("/content/assets/") ? game.videoUrl : null;

  useEffect(() => {
    if (!validVideoUrl || reducedMotion || !pageVisible || !inView) {return;}
    const timer = window.setTimeout(() => setReadyGameId(game.gameId), 700);
    return () => window.clearTimeout(timer);
  }, [game.gameId, inView, pageVisible, reducedMotion, validVideoUrl]);

  useEffect(() => {
    const onVisibility = () => setPageVisible(document.visibilityState === "visible");
    document.addEventListener("visibilitychange", onVisibility);
    return () => document.removeEventListener("visibilitychange", onVisibility);
  }, []);

  useEffect(() => {
    const element = stageRef.current;
    if (!element || typeof IntersectionObserver === "undefined") {return;}
    const observer = new IntersectionObserver(([entry]) => setInView(entry?.isIntersecting ?? false), { threshold: 0.25 });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  useEffect(() => () => {
    if (stalledTimer.current !== null) {window.clearTimeout(stalledTimer.current);}
  }, []);

  function failAfterStall() {
    if (stalledTimer.current !== null) {window.clearTimeout(stalledTimer.current);}
    stalledTimer.current = window.setTimeout(() => setVideoFailed(true), 3_000);
  }

  function markPlaying() {
    if (stalledTimer.current !== null) {window.clearTimeout(stalledTimer.current);}
    stalledTimer.current = null;
    setVideoPlaying(true);
  }

  const videoReady = readyGameId === game.gameId && pageVisible && inView;
  const showVideoPanel = Boolean(validVideoUrl && !videoFailed);
  return <div ref={stageRef} className={`${styles.mediaStage} ${showVideoPanel ? styles.withVideo : styles.coverOnly}`.trim()}>
    <Poster game={game} placeholder={!game.coverUrl} />
    {showVideoPanel ? <div className={styles.videoPanel} data-immersive-video-panel="true">
      {videoReady && !reducedMotion ? <video
        key={game.gameId}
        className={videoPlaying ? styles.videoPlaying : ""}
        src={validVideoUrl ?? undefined}
        muted
        playsInline
        loop
        preload="metadata"
        autoPlay
        aria-label={`${game.title} 游戏视频`}
        onPlaying={markPlaying}
        onError={() => setVideoFailed(true)}
        onStalled={failAfterStall}
      /> : <div className={styles.videoPoster} role="img" aria-label={`${game.title} 视频预览`}><span aria-hidden="true">▶</span><small>{reducedMotion ? "已关闭自动播放" : "视频即将播放"}</small></div>}
    </div> : null}
  </div>;
}
