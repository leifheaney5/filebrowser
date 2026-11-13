import { fetchJSON } from './utils';
import { getApiPath } from '@/utils/url';

const get = (path, query = {}) => fetchJSON(getApiPath(path, query));
const send = (path, body, method = 'POST') => fetchJSON(getApiPath(path), { method, body: JSON.stringify(body) });

export const organizerApi = {
  jobs: () => get('organizer/jobs'),
  createJob: body => send('organizer/jobs', body),
  cancelJob: id => send(`organizer/jobs/${encodeURIComponent(id)}/cancel`, {}),
  retryJob: id => send(`organizer/jobs/${encodeURIComponent(id)}/retry`, {}),
  trash: () => get('organizer/trash'),
  library: query => get('library', query),
  scan: body => send('library/scans', body),
  tags: body => send('library/tags', body, 'PATCH'),
};
