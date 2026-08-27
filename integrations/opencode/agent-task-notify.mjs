import { createAgentTaskNotify } from './bridge.mjs';
// The legacy host loader invokes every distinct export as a plugin.
export default createAgentTaskNotify();
