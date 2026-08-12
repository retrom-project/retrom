"use client";

import Image from "next/image";
import { useCallback, useEffect, useRef, useState } from "react";
import { AppIcon } from "@/components/app-icon";

const AUTO_PLAY_DELAY_MS = 2_000;
const PLAYING_TIMEOUT_MS = 5_000;

type MediaState = "cover" | "loading" | "video" | "paused" | "unavailable";

export function GameDetailMedia({ title, coverUrl, videoUrl }: { title: string; coverUrl: string | null; videoUrl: string | null }) {
  const stageRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);
  const timerRef = useRef<number | null>(null);
  const playingTimeoutRef = useRef<number | null>(null);
  const visibleSinceRef = useRef<number | null>(null);
  const visibleElapsedRef = useRef(0);
  const intersectingRef = useRef(false);
  const foregroundRef = useRef(typeof document === "undefined" || document.visibilityState === "visible");
  const userPausedRef = useRef(false);
  const reducedMotionRef = useRef(false);
  const mountedRef = useRef(true);
  const stateRef = useRef<MediaState>("cover");
  const [state, setState] = useState<MediaState>("cover");
  const [muted, setMuted] = useState(true);
  const [reducedMotion, setReducedMotion] = useState(false);

  const clearSchedule = useCallback(() => {
    if (timerRef.current !== null) window.clearTimeout(timerRef.current);
    timerRef.current = null;
    if (visibleSinceRef.current !== null) {
      visibleElapsedRef.current += Date.now() - visibleSinceRef.current;
      visibleSinceRef.current = null;
    }
  }, []);

  const clearPlayingTimeout = useCallback(() => {
    if (playingTimeoutRef.current !== null) window.clearTimeout(playingTimeoutRef.current);
    playingTimeoutRef.current = null;
  }, []);

  const fallBack = useCallback((unavailable = false) => {
    clearPlayingTimeout();
    videoRef.current?.pause();
    stateRef.current = unavailable ? "unavailable" : "cover";
    if (mountedRef.current) setState(stateRef.current);
  }, [clearPlayingTimeout]);

  const requestPlay = useCallback(async () => {
    const video = videoRef.current;
    if (!video || !videoUrl || userPausedRef.current) return;
    clearSchedule();
    clearPlayingTimeout();
    video.muted = muted;
    stateRef.current = "loading";
    if (mountedRef.current) setState("loading");
    playingTimeoutRef.current = window.setTimeout(() => fallBack(true), PLAYING_TIMEOUT_MS);
    try {
      await video.play();
    } catch {
      fallBack(true);
    }
  }, [clearPlayingTimeout, clearSchedule, fallBack, muted, videoUrl]);

  const schedule = useCallback(() => {
    clearSchedule();
    if (!videoUrl || reducedMotionRef.current || userPausedRef.current || stateRef.current === "video" || stateRef.current === "loading" || !intersectingRef.current || !foregroundRef.current) return;
    const remaining = Math.max(0, AUTO_PLAY_DELAY_MS - visibleElapsedRef.current);
    visibleSinceRef.current = Date.now();
    timerRef.current = window.setTimeout(() => {
      if (visibleSinceRef.current !== null) {
        visibleElapsedRef.current += Date.now() - visibleSinceRef.current;
        visibleSinceRef.current = null;
      }
      timerRef.current = null;
      void requestPlay();
    }, remaining);
  }, [clearSchedule, requestPlay, videoUrl]);

  useEffect(() => {
    mountedRef.current = true;
    if (!videoUrl) return () => { mountedRef.current = false; };
    const video = videoRef.current;
    const query = window.matchMedia("(prefers-reduced-motion: reduce)");
    const updateMotion = () => {
      reducedMotionRef.current = query.matches;
      setReducedMotion(query.matches);
      if (query.matches) {
        clearSchedule();
        fallBack(false);
      } else {
        schedule();
      }
    };
    updateMotion();
    query.addEventListener("change", updateMotion);
    return () => {
      mountedRef.current = false;
      query.removeEventListener("change", updateMotion);
      clearSchedule();
      clearPlayingTimeout();
      video?.pause();
    };
  }, [clearPlayingTimeout, clearSchedule, fallBack, schedule, videoUrl]);

  useEffect(() => {
    if (!videoUrl || !stageRef.current) return;
    const observer = new IntersectionObserver(([entry]) => {
      intersectingRef.current = Boolean(entry?.isIntersecting && entry.intersectionRatio > 0);
      if (intersectingRef.current) schedule();
      else {
        clearSchedule();
        if (stateRef.current === "video" || stateRef.current === "loading") fallBack(false);
      }
    }, { threshold: [0, 0.01] });
    observer.observe(stageRef.current);
    return () => observer.disconnect();
  }, [clearSchedule, fallBack, schedule, videoUrl]);

  useEffect(() => {
    if (!videoUrl) return;
    const onVisibility = () => {
      foregroundRef.current = document.visibilityState === "visible";
      if (foregroundRef.current) schedule();
      else {
        clearSchedule();
        if (stateRef.current === "video" || stateRef.current === "loading") fallBack(false);
      }
    };
    document.addEventListener("visibilitychange", onVisibility);
    return () => document.removeEventListener("visibilitychange", onVisibility);
  }, [clearSchedule, fallBack, schedule, videoUrl]);

  function togglePlayback() {
    if (state === "video" || state === "loading") {
      userPausedRef.current = true;
      clearSchedule();
      clearPlayingTimeout();
      videoRef.current?.pause();
      stateRef.current = "paused";
      setState("paused");
      return;
    }
    userPausedRef.current = false;
    visibleElapsedRef.current = AUTO_PLAY_DELAY_MS;
    void requestPlay();
  }

  const status = !videoUrl ? "正在展示封面" : state === "video" ? "正在循环播放视频预览" : state === "loading" ? "正在载入视频预览" : state === "unavailable" ? "此视频无法在当前浏览器播放，已恢复封面" : state === "paused" ? "视频预览已暂停" : reducedMotion ? "已减少动态效果，可手动播放视频预览" : "正在展示封面 · 可见满 2 秒后播放视频";

  return <div className="game-detail-media">
    <div className="game-detail-poster" ref={stageRef}>
      {coverUrl ? <Image className="game-detail-media-cover" src={coverUrl} alt={`${title} 封面`} fill sizes="240px" priority unoptimized /> : <div className="game-detail-media-placeholder" role="img" aria-label={`${title} 暂无封面`}><span>{title}</span></div>}
      {videoUrl ? <video
        ref={videoRef}
        className={`game-detail-media-video${state === "video" ? " is-playing" : ""}`}
        src={videoUrl}
        aria-label={`${title} 视频预览`}
        muted={muted}
        playsInline
        loop
        preload="metadata"
        onPlaying={() => { clearPlayingTimeout(); stateRef.current = "video"; setState("video"); }}
        onError={() => fallBack(true)}
        onStalled={() => fallBack(true)}
      /> : null}
      {videoUrl ? <div className="game-detail-media-controls">
        <button type="button" onClick={togglePlayback}><AppIcon name={state === "video" || state === "loading" ? "pause" : "play"} />{state === "video" || state === "loading" ? "暂停预览" : "播放视频预览"}</button>
        <button type="button" aria-pressed={!muted} onClick={() => { const next = !muted; setMuted(next); if (videoRef.current) videoRef.current.muted = next; }}>{muted ? "已静音" : "开启声音"}</button>
      </div> : null}
    </div>
    <p className="game-detail-media-status" aria-live="polite">{status}</p>
  </div>;
}
