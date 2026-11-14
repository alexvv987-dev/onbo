export interface User {
  id: string;
  name: string;
  team: string;
}
export interface Config {
  demoMode: boolean;
  llmMode: string;
}
export interface Task {
  id: string;
  title: string;
  description: string;
  status: string;
  manual: boolean;
}
export interface Plan {
  tasks: Task[];
  nextStep: string;
}
export interface Source {
  id: string;
  title: string;
  excerpt: string;
  label: string;
}
export interface Draft {
  id: string;
  resourceId: string;
  resourceName: string;
  reason: string;
}
export interface AccessRequest {
  id: string;
  draftId: string;
  resourceId: string;
  resourceName: string;
  status: string;
  message: string;
}
export interface Message {
  role: string;
  text: string;
  sources?: Source[];
  draft?: Draft;
}
export interface ChatReply {
  message: string;
  sources: Source[];
  draft?: Draft;
}
