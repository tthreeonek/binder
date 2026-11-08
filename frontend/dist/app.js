'use strict';

const api = () => window.go.main.App;
const $ = (s) => document.querySelector(s);

let state = null;      // { config, errors, version, path }
let recording = null;  // { target, chip, mods }
let modalData = null;  // { id|null, hotkey }

// ---------- отображение сочетаний ----------

const KEY_NAMES = {
  Backquote: '`', Minus: '-', Equal: '=', BracketLeft: '[', BracketRight: ']',
  Backslash: '\\', Semicolon: ';', Quote: "'", Comma: ',', Period: '.', Slash: '/',
  Space: 'Space', Insert: 'Ins', Delete: 'Del', Home: 'Home', End: 'End',
  PageUp: 'PgUp', PageDown: 'PgDn', ArrowUp: '↑', ArrowDown: '↓', ArrowLeft: '←', ArrowRight: '→',
  NumpadMultiply: 'Num *', NumpadAdd: 'Num +', NumpadSubtract: 'Num -',
  NumpadDecimal: 'Num .', NumpadDivide: 'Num /', Pause: 'Pause', ScrollLock: 'ScrLk',
};

function keyName(code) {
  if (/^Digit\d$/.test(code)) return code.slice(5);
  if (/^Key[A-Z]$/.test(code)) return code.slice(3);
  if (/^Numpad\d$/.test(code)) return 'Num ' + code.slice(6);
  if (/^F\d{1,2}$/.test(code)) return code;
  return KEY_NAMES[code] || null;
}

function modList(h) {
  const m = [];
  if (h.ctrl) m.push('Ctrl');
  if (h.alt) m.push('Alt');
  if (h.shift) m.push('Shift');
  if (h.win) m.push('Win');
  return m;
}

function hkText(h) {
  if (!h || !h.code) return '';
  return [...modList(h), keyName(h.code) || h.code].join(' + ');
}

const sameHk = (a, b) => !!(a && b && a.code && a.code === b.code &&
  !!a.ctrl === !!b.ctrl && !!a.alt === !!b.alt && !!a.shift === !!b.shift && !!a.win === !!b.win);

const emptyHk = () => ({ ctrl: false, alt: false, shift: false, win: false, code: '' });

// ---------- уведомления ----------

let toastTimer;
function toast(text, err = false) {
  const t = $('#toast');
  t.textContent = text;
  t.classList.toggle('err', err);
  t.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.remove('show'), err ? 4200 : 2400);
}

// ---------- сохранение ----------

async function save(msg) {
  try {
    state = await api().SaveConfig(state.config);
    render();
    const errs = Object.values(state.errors || {});
    if (errs.length) toast('Не все сочетания заняты: ' + errs[0], true);
    else if (msg) toast(msg);
  } catch (e) {
    toast(String(e), true);
  }
}

// ---------- список фраз ----------

const ICONS = {
  edit: '<svg viewBox="0 0 24 24"><path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z"/></svg>',
  copy: '<svg viewBox="0 0 24 24"><rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>',
  del: '<svg viewBox="0 0 24 24"><path d="M3 6h18M8 6V4h8v2M6 6l1 14h10l1-14"/></svg>',
  drag: '<svg viewBox="0 0 24 24"><circle cx="9" cy="6" r="1"/><circle cx="15" cy="6" r="1"/><circle cx="9" cy="12" r="1"/><circle cx="15" cy="12" r="1"/><circle cx="9" cy="18" r="1"/><circle cx="15" cy="18" r="1"/></svg>',
};

function chipFor(el, hk, errKey) {
  const txt = hkText(hk);
  el.textContent = txt || 'Назначить';
  el.classList.toggle('empty', !txt);
  const err = errKey && state.errors && state.errors[errKey];
  el.classList.toggle('error', !!err);
  el.title = err ? err : 'Нажмите, чтобы назначить другое сочетание';
}

function render() {
  const c = state.config;
  const s = c.settings;

  // список
  const list = $('#list');
  list.innerHTML = '';
  if (!c.phrases.length) {
    list.innerHTML = '<div class="empty-state">Фраз пока нет — добавьте первую.</div>';
  }
  c.phrases.forEach((p, i) => {
    const card = document.createElement('div');
    card.className = 'card';
    card.dataset.id = p.id;
    card.innerHTML = `
      <div class="text" title="Нажмите, чтобы вставить в окно, где вы были"></div>
      <div class="side">
        <button class="chip" data-bind="${p.id}"></button>
        <div class="tools">
          <button class="icon-btn drag-handle" title="Перетащите, чтобы поменять порядок" draggable="true">${ICONS.drag}</button>
          <button class="icon-btn" data-act="copy" title="Скопировать">${ICONS.copy}</button>
          <button class="icon-btn" data-act="edit" title="Изменить">${ICONS.edit}</button>
          <button class="icon-btn del" data-act="del" title="Удалить">${ICONS.del}</button>
        </div>
      </div>`;
    card.querySelector('.text').textContent = p.text;
    chipFor(card.querySelector('.chip'), p.hotkey, p.id);
    list.appendChild(card);
  });

  const th = hkText(s.toggleHotkey);
  $('#toggleHint').innerHTML = th
    ? `Открыть / скрыть: <span class="kbd">${th}</span> · клик по фразе — вставить`
    : 'Сочетание для открытия не задано — назначьте его в настройках';

  // настройки
  chipFor(document.querySelector('[data-bind="toggle"]'), s.toggleHotkey, 'toggle');
  $('#opacity').value = 100 - s.opacity;
  $('#checkUpdates').checked = s.checkUpdates;
  $('#repoLink').textContent = state.repo.replace('https://', '');
  if (!updInfo) $('#updStatus').textContent = 'Версия ' + state.version;
  $('#alwaysOnTop').checked = s.alwaysOnTop;
  $('#showWithoutFocus').checked = s.showWithoutFocus;
  $('#restoreClipboard').checked = s.restoreClipboard;
  $('#pressEnter').checked = s.pressEnter;
  document.querySelectorAll('#pasteMode button').forEach((b) =>
    b.classList.toggle('on', b.dataset.mode === s.pasteMode));
  $('#btnPin').classList.toggle('on', s.alwaysOnTop);
  $('#cfgPath').textContent = state.path;
  $('#ver').textContent = 'v' + state.version;
  $('#footerInfo').textContent = `Фраз: ${c.phrases.length} · v${state.version}`;
  applyOpacity();
}

function applyOpacity() {
  const t = 100 - state.config.settings.opacity;
  $('#opVal').textContent = t ? t + '%' : 'выкл';
}

const findPhrase = (id) => state.config.phrases.find((p) => p.id === id);

// ---------- запись сочетания ----------

async function startRecording(target, chip) {
  if (recording) await stopRecording();
  recording = { target, chip };
  chip.classList.add('recording');
  chip.classList.remove('empty', 'error');
  chip.textContent = 'Нажмите сочетание…';
  chip.title = 'Esc — отмена, Backspace — убрать сочетание';
  try { await api().SetRecording(true); } catch (_) {}
}

async function stopRecording() {
  if (!recording) return;
  recording.chip.classList.remove('recording');
  recording = null;
  try { await api().SetRecording(false); } catch (_) {}
}

function currentHk(target) {
  if (target === 'toggle') return state.config.settings.toggleHotkey;
  if (target === 'modal') return modalData.hotkey;
  return findPhrase(target).hotkey;
}

async function finishRecording(hk) {
  const target = recording.target;
  await stopRecording();
  const s = state.config.settings;

  if (target === 'modal') {
    modalData.hotkey = hk;
    chipFor(document.querySelector('[data-bind="modal"]'), hk);
    return;
  }

  let moved = '';
  if (hk.code) {
    if (target !== 'toggle' && sameHk(hk, s.toggleHotkey)) {
      toast(`${hkText(hk)} уже открывает биндер — выберите другое`, true);
      render();
      return;
    }
    // Одно сочетание — одна фраза: у прежнего владельца его снимаем.
    for (const p of state.config.phrases) {
      if (p.id !== target && sameHk(p.hotkey, hk)) {
        p.hotkey = emptyHk();
        moved = p.text;
      }
    }
  }
  if (target === 'toggle') s.toggleHotkey = hk;
  else findPhrase(target).hotkey = hk;

  const short = (t) => (t.length > 28 ? t.slice(0, 28) + '…' : t);
  await save(hk.code
    ? (moved ? `${hkText(hk)} перенесено с «${short(moved)}»` : `Назначено: ${hkText(hk)}`)
    : 'Сочетание убрано');
}

function onRecordKey(e) {
  e.preventDefault();
  e.stopPropagation();
  if (e.type !== 'keydown') return;
  const mods = { ctrl: e.ctrlKey, alt: e.altKey, shift: e.shiftKey, win: e.metaKey };
  const noMods = !mods.ctrl && !mods.alt && !mods.shift && !mods.win;

  if (e.code === 'Escape' && noMods) {
    stopRecording().then(() => (recording === null && !modalData ? render() : refreshModalChip()));
    return;
  }
  if ((e.code === 'Backspace' || e.code === 'Delete') && noMods) {
    finishRecording(emptyHk());
    return;
  }
  if (['ControlLeft', 'ControlRight', 'AltLeft', 'AltRight', 'ShiftLeft', 'ShiftRight', 'MetaLeft', 'MetaRight'].includes(e.code)) {
    const m = modList(mods);
    recording.chip.textContent = m.length ? m.join(' + ') + ' + …' : 'Нажмите сочетание…';
    return;
  }
  if (!keyName(e.code)) {
    toast('Эту клавишу назначить нельзя', true);
    return;
  }
  const isF = /^F\d{1,2}$/.test(e.code);
  if (!isF && !mods.ctrl && !mods.alt && !mods.win) {
    toast('Добавьте Ctrl или Alt — иначе клавиша перестанет печататься', true);
    return;
  }
  finishRecording({ ...mods, code: e.code });
}

function refreshModalChip() {
  if (modalData) chipFor(document.querySelector('[data-bind="modal"]'), modalData.hotkey);
  else render();
}

window.addEventListener('keydown', (e) => { if (recording) onRecordKey(e); }, true);
window.addEventListener('keyup', (e) => { if (recording) { e.preventDefault(); e.stopPropagation(); } }, true);
window.addEventListener('blur', () => { if (recording) stopRecording().then(refreshModalChip); });

// ---------- окно фразы ----------

function openModal(id) {
  const p = id ? findPhrase(id) : null;
  modalData = { id, hotkey: p ? { ...p.hotkey } : nextFreeHotkey() };
  $('#modalTitle').textContent = p ? 'Изменить фразу' : 'Новая фраза';
  $('#modalText').value = p ? p.text : '';
  chipFor(document.querySelector('[data-bind="modal"]'), modalData.hotkey);
  $('#modal').classList.remove('hidden');
  setTimeout(() => $('#modalText').focus(), 30);
}

function closeModal() {
  if (recording) stopRecording();
  modalData = null;
  $('#modal').classList.add('hidden');
}

// Следующее свободное Alt + цифра для новой фразы.
function nextFreeHotkey() {
  for (let d = 1; d <= 9; d++) {
    const hk = { ...emptyHk(), alt: true, code: 'Digit' + d };
    if (!state.config.phrases.some((p) => sameHk(p.hotkey, hk)) && !sameHk(hk, state.config.settings.toggleHotkey)) return hk;
  }
  return emptyHk();
}

async function saveModal() {
  const text = $('#modalText').value.replace(/\s+$/, '');
  if (!text.trim()) { toast('Введите текст фразы', true); return; }
  const hk = modalData.hotkey;
  if (hk.code && sameHk(hk, state.config.settings.toggleHotkey)) {
    toast(`${hkText(hk)} уже открывает биндер`, true);
    return;
  }
  let p = modalData.id ? findPhrase(modalData.id) : null;
  if (!p) {
    p = { id: 'p' + Date.now().toString(36), text: '', hotkey: emptyHk() };
    state.config.phrases.push(p);
  }
  let moved = '';
  if (hk.code) {
    for (const o of state.config.phrases) {
      if (o !== p && sameHk(o.hotkey, hk)) { o.hotkey = emptyHk(); moved = hkText(hk); }
    }
  }
  p.text = text;
  p.hotkey = hk;
  closeModal();
  await save(moved ? `Сохранено, ${moved} перенесено на эту фразу` : 'Сохранено');
}

// ---------- события ----------

document.addEventListener('click', async (e) => {
  const chip = e.target.closest('.chip[data-bind]');
  if (chip) {
    const target = chip.dataset.bind;
    if (recording && recording.target === target) { await stopRecording(); refreshModalChip(); return; }
    startRecording(target, chip);
    return;
  }
  if (recording && !e.target.closest('.chip')) { await stopRecording(); refreshModalChip(); }

  const card = e.target.closest('.card');
  if (card) {
    const id = card.dataset.id;
    const act = e.target.closest('[data-act]');
    if (act) {
      if (act.dataset.act === 'edit') openModal(id);
      if (act.dataset.act === 'copy') {
        try { await api().CopyPhrase(id); toast('Скопировано в буфер'); } catch (err) { toast(String(err), true); }
      }
      if (act.dataset.act === 'del') {
        const p = findPhrase(id);
        state.config.phrases = state.config.phrases.filter((x) => x.id !== id);
        lastDeleted = { p, index: [...document.querySelectorAll('.card')].indexOf(card) };
        await save();
        toastUndo();
      }
      return;
    }
    if (e.target.closest('.text')) {
      try { await api().PastePhrase(id); } catch (err) { toast(String(err), true); }
    }
  }
});

let lastDeleted = null;
function toastUndo() {
  const t = $('#toast');
  toast('Фраза удалена — Ctrl + Z, чтобы вернуть');
  t.dataset.undo = '1';
}
document.addEventListener('keydown', async (e) => {
  if (recording) return;
  if (e.ctrlKey && e.code === 'KeyZ' && lastDeleted && document.activeElement.tagName !== 'TEXTAREA') {
    state.config.phrases.splice(Math.max(0, lastDeleted.index), 0, lastDeleted.p);
    lastDeleted = null;
    await save('Фраза возвращена');
  }
  if (!$('#modal').classList.contains('hidden')) {
    if (e.code === 'Escape') closeModal();
    if (e.code === 'Enter' && e.ctrlKey) saveModal();
  }
});

$('#btnAdd').onclick = () => openModal(null);
$('#modalCancel').onclick = closeModal;
$('#modalSave').onclick = saveModal;
$('#modal').addEventListener('mousedown', (e) => { if (e.target.id === 'modal') closeModal(); });

$('#btnSettings').onclick = () => {
  const settingsOpen = !$('#viewSettings').classList.contains('hidden');
  $('#viewSettings').classList.toggle('hidden', settingsOpen);
  $('#viewList').classList.toggle('hidden', !settingsOpen);
  $('#btnSettings').classList.toggle('on', !settingsOpen);
};
$('#btnHide').onclick = () => api().Hide();
$('#btnClose').onclick = () => api().Quit();
$('#btnPin').onclick = () => {
  state.config.settings.alwaysOnTop = !state.config.settings.alwaysOnTop;
  save(state.config.settings.alwaysOnTop ? 'Окно поверх всех' : 'Обычное окно');
};

let sliderTimer;
$('#opacity').addEventListener('input', (e) => {
  const s = state.config.settings;
  s.opacity = 100 - +e.target.value;
  applyOpacity();
  api().SetOpacity(s.opacity);
  clearTimeout(sliderTimer);
  sliderTimer = setTimeout(() => save(), 400);
});
for (const key of ['alwaysOnTop', 'showWithoutFocus', 'restoreClipboard', 'pressEnter', 'checkUpdates']) {
  $('#' + key).addEventListener('change', (e) => {
    state.config.settings[key] = e.target.checked;
    save();
  });
}
document.querySelectorAll('#pasteMode button').forEach((b) => {
  b.onclick = () => { state.config.settings.pasteMode = b.dataset.mode; save(); };
});
$('#btnReset').onclick = async () => {
  if ($('#btnReset').dataset.confirm !== '1') {
    $('#btnReset').dataset.confirm = '1';
    $('#btnReset').textContent = 'Точно сбросить? Нажмите ещё раз';
    setTimeout(() => { $('#btnReset').dataset.confirm = ''; $('#btnReset').textContent = 'Сбросить фразы и настройки'; }, 3000);
    return;
  }
  try { state = await api().ResetConfig(); render(); toast('Сброшено'); } catch (e) { toast(String(e), true); }
};

// ---------- обновления ----------

var updInfo = null;

function showUpdate(info) {
  updInfo = info;
  $('#updVer').textContent = info.version;
  $('#updBanner').classList.remove('hidden');
  $('#updStatus').textContent = `Доступна ${info.version} (у вас ${info.current})`;
}

$('#updNotes').onclick = () => updInfo && api().OpenURL(updInfo.page);
$('#repoLink').onclick = () => api().OpenURL(state.repo);

$('#btnUpdate').onclick = async () => {
  const b = $('#btnUpdate');
  b.disabled = true;
  b.textContent = 'Скачиваю…';
  try {
    await api().InstallUpdate();
    b.textContent = 'Перезапуск…';
  } catch (e) {
    b.disabled = false;
    b.textContent = 'Обновить';
    toast(String(e), true);
  }
};

$('#btnCheckUpd').onclick = async () => {
  const b = $('#btnCheckUpd');
  b.disabled = true;
  try {
    const info = await api().CheckUpdate();
    if (info.available) { showUpdate(info); toast(`Доступна версия ${info.version}`); }
    else toast('У вас последняя версия');
  } catch (e) {
    toast(String(e), true);
  }
  b.disabled = false;
};

// ---------- перетаскивание ----------

let dragId = null;
document.addEventListener('dragstart', (e) => {
  const card = e.target.closest('.card');
  if (!card) return;
  dragId = card.dataset.id;
  card.classList.add('dragging');
  e.dataTransfer.effectAllowed = 'move';
  e.dataTransfer.setData('text/plain', dragId);
  e.dataTransfer.setDragImage(card, 20, 20);
});
document.addEventListener('dragover', (e) => {
  if (!dragId) return;
  e.preventDefault();
  document.querySelectorAll('.drop-before').forEach((c) => c.classList.remove('drop-before'));
  const card = e.target.closest('.card');
  if (card && card.dataset.id !== dragId) card.classList.add('drop-before');
});
document.addEventListener('drop', (e) => {
  if (!dragId) return;
  e.preventDefault();
  const card = e.target.closest('.card');
  const list = state.config.phrases;
  const from = list.findIndex((p) => p.id === dragId);
  const [item] = list.splice(from, 1);
  let to = card ? list.findIndex((p) => p.id === card.dataset.id) : list.length;
  if (to < 0) to = list.length;
  list.splice(to, 0, item);
  dragId = null;
  save();
});
document.addEventListener('dragend', () => {
  dragId = null;
  document.querySelectorAll('.dragging, .drop-before').forEach((c) => c.classList.remove('dragging', 'drop-before'));
});

// ---------- запуск ----------

async function init() {
  if (!window.go) { setTimeout(init, 30); return; }
  state = await api().GetState();
  render();
  const errs = Object.values(state.errors || {});
  if (errs.length) toast('Не все сочетания заняты: ' + errs[0], true);

  window.runtime.EventsOn('binder:pasted', (id) => {
    const card = document.querySelector(`.card[data-id="${CSS.escape(id)}"]`);
    if (!card) return;
    card.classList.add('flash');
    setTimeout(() => card.classList.remove('flash'), 450);
  });
  window.runtime.EventsOn('binder:error', (msg) => toast(msg, true));
  window.runtime.EventsOn('binder:update', showUpdate);
  window.runtime.EventsOn('binder:update-progress', (pct) => {
    $('#btnUpdate').textContent = `Скачиваю ${pct}%`;
  });
  if (state.updated) toast(`Биндер обновлён до версии ${state.version}`);
}

document.addEventListener('contextmenu', (e) => { if (e.target.tagName !== 'TEXTAREA') e.preventDefault(); });
init();
