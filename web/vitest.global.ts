// Runs once in Vitest's main process, before the workers start. The vmThreads
// pool runs test files in worker threads, which share the process's time zone:
// vi.stubEnv("TZ", …) inside a test no longer changes it. The tests that
// convert between local time and UTC are written for America/New_York, so the
// zone is fixed here, whatever the machine's.
export default function setup() {
  (globalThis as unknown as { process: { env: Record<string, string> } }).process.env.TZ = "America/New_York";
}
