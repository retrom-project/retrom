"use client";

import {useCallback, useRef, useState, type Dispatch, type SetStateAction} from "react";
import type {RuntimeStartupTaskV1} from "./runtime/contract";
import {StartupTimeline} from "./startup-task-model";

export function useStartupTasks(setMessage: Dispatch<SetStateAction<string>>) {
  const timeline = useRef(new StartupTimeline());
  const [startupTasks, setStartupTasks] = useState<RuntimeStartupTaskV1[]>([]);
  const reportStartupTask = useCallback((task: RuntimeStartupTaskV1 | null) => {
    if (!task) {timeline.current = new StartupTimeline(); setStartupTasks([]); return;}
    setStartupTasks(timeline.current.receive(task));
    setMessage(timeline.current.message());
  }, [setMessage]);
  return {startupTasks, reportStartupTask};
}
