import { useState, useEffect } from 'react';
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime';

export interface IndexingStatus {
  totalFiles: number;
  indexedFiles: number;
  failedFiles: number;
  currentFile: string;
  isRunning: boolean;
  paused: boolean;
  quotaPaused: boolean;
  quotaResumeAt: string;
}

const defaultStatus: IndexingStatus = {
  totalFiles: 0,
  indexedFiles: 0,
  failedFiles: 0,
  currentFile: '',
  isRunning: false,
  paused: false,
  quotaPaused: false,
  quotaResumeAt: '',
};

export function useIndexingStatus() {
  const [status, setStatus] = useState<IndexingStatus>(defaultStatus);

  useEffect(() => {
    EventsOn('indexing-status', (data: IndexingStatus) => {
      setStatus(data);
    });

    return () => {
      EventsOff('indexing-status');
    };
  }, []);

  return status;
}
