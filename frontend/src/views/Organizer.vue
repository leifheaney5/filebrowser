<template>
  <div class="organizer" @keydown="onKeys">
    <aside class="workspace-nav" aria-label="Workspace navigation">
      <router-link class="workspace-brand" to="/workspace"><span class="brand-mark">{{ $t('organizer.f') }}</span><span>{{ $t('organizer.file') }}<span class="brand-light">{{ $t('organizer.space') }}</span></span></router-link>
      <p class="nav-caption">{{ $t('organizer.yourWorkspace') }}</p>
      <nav>
        <button type="button" v-for="entry in sections" :key="entry.id" :class="{ active: tab === entry.id }" @click="switchTab(entry.id)"><i class="material-symbols">{{ entry.icon }}</i>{{ entry.name }}<span v-if="entry.id === 'activity' && runningJobs" class="nav-count">{{ runningJobs }}</span></button>
      </nav>
      <div class="nav-heading"><p class="nav-caption">{{ $t('organizer.pinnedLocations') }}</p><button type="button" title="Pin this folder" aria-label="Pin this folder" :disabled="!source || tab !== 'files'" @click="toggleFavorite"><i class="material-symbols">add</i></button></div>
      <button type="button" v-for="favorite in favorites" :key="itemKey(favorite)" class="favorite-link" :title="`${favorite.source} · ${favorite.path}`" @click="navigate(favorite.source, favorite.path)"><i class="material-symbols">folder</i><span>{{ favorite.name }}</span></button>
      <p v-if="!favorites.length" class="nav-hint">{{ $t('organizer.keepYourEverydayFoldersClosePinOneAsYouBrowse') }}</p>
      <div class="nav-bottom"><span class="connection-dot"></span><span>{{ $t('organizer.leifWorkspace') }}</span><router-link to="/settings" aria-label="Settings" title="Settings"><i class="material-symbols">settings</i></router-link></div>
    </aside>

    <main class="workspace-main">
      <header class="workspace-top"><span class="workspace-eyebrow">{{ $t('organizer.aLittleRoomForEverything') }}</span><div class="top-actions"><button type="button" @click="openTransfers"><i class="material-symbols">sync_alt</i><span>{{ $t('organizer.transfers') }}</span><span v-if="transferCount" class="nav-count">{{ transferCount }}</span></button><router-link :to="source ? buildItemUrl(source, path) : '/files/'" title="Open original interface"><i class="material-symbols">open_in_new</i></router-link><span class="user-avatar" :title="state.user.username">{{ userInitial }}</span></div></header>
      <section class="workspace-content">
        <div class="page-heading"><div><p class="eyebrow">{{ sectionSubtitle }}</p><h1>{{ pageTitle }}</h1><p class="page-description">{{ sectionDescription }}</p></div><button type="button" v-if="tab === 'files' && canCreate" class="primary" @click="upload"><i class="material-symbols">upload</i>{{ $t('organizer.uploadFiles') }}</button><button type="button" v-if="tab === 'library'" class="primary" :disabled="busy || scanRunning || !source" @click="startScan"><i class="material-symbols">refresh</i>{{ scanRunning ? $t('organizer.scanning') : $t('organizer.indexThisFolder') }}</button></div>
        <div v-if="error" class="workspace-error" role="alert"><i class="material-symbols">error</i><span>{{ error }}</span><button type="button" aria-label="Dismiss error" @click="error = ''">{{ $t('organizer.separator') }}</button></div>
        <p v-if="notice" class="workspace-notice" role="status">{{ notice }}</p>

        <template v-if="tab === 'files' || tab === 'library'">
          <div class="location-bar"><select v-model="source" aria-label="Storage source" @change="changeSource"><option v-for="name in sources" :key="name" :value="name">{{ name }}</option></select><span class="location-divider">{{ $t('organizer.separator2') }}</span><form class="address-form" @submit.prevent="navigate(source, address)"><input ref="addressInput" v-model="address" aria-label="Folder path" title="Folder path · Ctrl+L"/><button type="submit" title="Go to folder" aria-label="Go to folder"><i class="material-symbols">arrow_forward</i></button></form><button type="button" :class="{ pinned: isPinned }" :aria-pressed="isPinned" :title="isPinned ? 'Unpin folder' : 'Pin folder'" aria-label="Toggle favorite" @click="toggleFavorite"><i class="material-symbols" :textContent="isPinned ? 'star' : 'star_border'" aria-hidden="true"></i></button><button type="button" title="Parent folder" aria-label="Parent folder" :disabled="path === '/'" @click="navigate(source, getParentDir(path))"><i class="material-symbols">arrow_upward</i></button><button type="button" title="Refresh" aria-label="Refresh" :disabled="busy" @click="refresh(true)"><i class="material-symbols">refresh</i></button></div>
          <div class="browser-tools">
            <label class="filter-input">
              <i class="material-symbols" aria-hidden="true">search</i>
              <input
                v-model="query"
                :placeholder="tab === 'library' ? 'Search names, paths, tags…' : 'Find in this folder…'"
                :aria-label="tab === 'library' ? 'Search indexed library' : 'Filter this folder'"
              />
              <kbd :textContent="'/'"></kbd>
            </label>
            <div class="browser-filter-controls" role="group" aria-label="Filters">
              <select v-if="tab === 'files'" v-model="category" aria-label="Filter by type">
                <option value="">{{ $t('organizer.allTypes') }}</option>
                <option value="directory">{{ $t('organizer.folders') }}</option>
                <option value="image">{{ $t('organizer.images') }}</option>
                <option value="video">{{ $t('organizer.video') }}</option>
                <option value="audio">{{ $t('organizer.audio') }}</option>
                <option value="text">{{ $t('organizer.text') }}</option>
                <option value="application">{{ $t('organizer.documentsOther') }}</option>
              </select>
              <button
                type="button"
                v-if="tab === 'files'"
                class="filter-toggle"
                :class="{ active: showFilters }"
                :aria-expanded="showFilters"
                @click="showFilters = !showFilters"
              >
                <i class="material-symbols" aria-hidden="true">tune</i>
                {{ $t('organizer.moreFilters') }}
                <span v-if="activeFilterCount" class="filter-count">{{ activeFilterCount }}</span>
              </button>
            </div>
            <div class="browser-view-controls" role="group" :aria-label="$t('organizer.sortBy')">
              <select v-model="sort" :aria-label="$t('organizer.sortBy')">
                <option value="name">{{ $t('organizer.name') }}</option>
                <option value="modified">{{ $t('organizer.modifiedDate') }}</option>
                <option value="size">{{ $t('organizer.fileSize') }}</option>
                <option value="type">{{ $t('organizer.type') }}</option>
                <template v-if="tab === 'library'">
                  <option value="capturedAt">{{ $t('organizer.captureDate') }}</option>
                  <option value="width">{{ $t('organizer.width') }}</option>
                  <option value="height">{{ $t('organizer.height') }}</option>
                  <option value="duration">{{ $t('organizer.duration') }}</option>
                  <option value="artist">{{ $t('organizer.artist') }}</option>
                  <option value="album">{{ $t('organizer.album') }}</option>
                  <option value="title">{{ $t('organizer.title') }}</option>
                  <option value="tags">{{ $t('organizer.tags') }}</option>
                </template>
                <option v-else value="extension">{{ $t('organizer.extension') }}</option>
              </select>
              <button
                type="button"
                :aria-label="direction === 'asc' ? 'Sort descending' : 'Sort ascending'"
                :title="direction === 'asc' ? 'Sort descending' : 'Sort ascending'"
                @click="direction = direction === 'asc' ? 'desc' : 'asc'"
              >
                <i class="material-symbols" :textContent="direction === 'asc' ? 'arrow_upward' : 'arrow_downward'" aria-hidden="true"></i>
              </button>
              <select v-model="density" aria-label="Row density">
                <option value="comfortable">{{ $t('organizer.comfortable') }}</option>
                <option value="compact">{{ $t('organizer.compact') }}</option>
              </select>
              <button
                type="button"
                v-if="tab === 'files'"
                :class="{ pinned: split }"
                :aria-pressed="split"
                title="Destination pane"
                aria-label="Toggle destination pane"
                @click="toggleSplit"
              >
                <i class="material-symbols" aria-hidden="true">view_column</i>
              </button>
            </div>
          </div>
          <section v-if="showFilters && tab === 'files'" class="filter-panel" :aria-label="$t('organizer.moreFilters')"><div><label>{{ $t('organizer.minimumSizeMb') }}<input v-model="minSize" type="number" min="0" step="0.1" inputmode="decimal" placeholder="0"/></label><label>{{ $t('organizer.maximumSizeMb') }}<input v-model="maxSize" type="number" min="0" step="0.1" inputmode="decimal" placeholder="Any"/></label><label>{{ $t('organizer.modifiedAfter') }}<input v-model="modifiedAfter" type="date" :max="modifiedBefore || undefined"/></label><label>{{ $t('organizer.modifiedBefore') }}<input v-model="modifiedBefore" type="date" :min="modifiedAfter || undefined"/></label><button v-if="activeFilterCount" type="button" class="clear-filters" @click="clearFilters">{{ $t('organizer.clearFilters') }}</button></div><p>{{ $t('organizer.foldersRemainVisible') }}</p></section>
          <div v-if="tab === 'library'" class="index-status" role="status" aria-live="polite">
            <i class="material-symbols" :textContent="['queued', 'running'].includes(scan?.status) ? 'sync' : 'info'" aria-hidden="true"></i>
            <div class="index-details">
              <strong v-if="scan" class="scan-state" :class="`scan-state--${scan.status}`">{{ $t('organizer.index') }}{{ scan.status }}</strong>
              <span class="scan-scope">{{ scan?.source || source }}{{ $t('organizer.separator3') }}{{ scan?.path || path }}</span>
              <span v-if="scan">{{ scan.processed || 0 }}{{ $t('organizer.filesProcessed') }}<template v-if="scan.errors">{{ $t('organizer.separator3') }}{{ scan.errors }}{{ $t('organizer.issues') }}</template></span>
              <span v-else>{{ $t('organizer.indexAFolderToSearchItsSubfoldersCompareContentAnd') }}</span>
              <span v-if="scan?.error" class="scan-error">{{ $t('organizer.separator4') }}{{ scan.error }}</span>
            </div>
            <button
              type="button"
              v-if="duplicates.length"
              class="duplicate-toggle"
              :aria-expanded="showDuplicates"
              aria-controls="workspace-duplicate-review"
              @click="showDuplicates = !showDuplicates"
            >
              {{ duplicates.length }}{{ $t('organizer.duplicateGroups') }}
              <i class="material-symbols" :textContent="showDuplicates ? 'expand_less' : 'expand_more'" aria-hidden="true"></i>
            </button>
          </div>
          <section
            v-if="tab === 'library' && duplicates.length"
            v-show="showDuplicates"
            id="workspace-duplicate-review"
            class="duplicates-panel"
            aria-label="Content duplicates"
          >
            <div class="section-heading">
              <h2>{{ $t('organizer.sameContentYourChoice') }}</h2>
              <span>{{ $t('organizer.sha256VerifiedAtIndexingCheckedAgainBeforeOperations') }}</span>
            </div>
            <article v-for="group in duplicates" :key="group.sha256" class="duplicate-group">
              <div class="duplicate-summary">
                <i class="material-symbols" aria-hidden="true">content_copy</i>
                <strong>{{ group.files.length }}{{ $t('organizer.identicalFiles') }}</strong>
                <span>{{ formatBytes(group.size) }}{{ $t('organizer.each') }}</span>
                <button type="button" class="select-other-copies" @click="selectDuplicates(group)">{{ $t('organizer.selectOtherCopies') }}</button>
              </div>
              <label
                v-for="file in group.files"
                :key="itemKey(file)"
                class="duplicate-file"
                :class="{ 'is-keeper': (keepers[group.sha256] || itemKey(group.files[0])) === itemKey(file) }"
              >
                <input
                  type="radio"
                  :name="group.sha256"
                  :checked="(keepers[group.sha256] || itemKey(group.files[0])) === itemKey(file)"
                  :aria-label="`${$t('organizer.keep')}: ${file.source} ${file.path}`"
                  @change="chooseKeeper(group, file)"
                />
                <span class="keeper-label">{{ (keepers[group.sha256] || itemKey(group.files[0])) === itemKey(file) ? $t('organizer.keep') : '' }}</span>
                <span class="duplicate-path">{{ file.source }}{{ $t('organizer.separator3') }}{{ file.path }}</span>
              </label>
            </article>
          </section>
          <div class="browser-layout" :class="{ 'with-side': split || preview }">
            <section class="file-surface" aria-label="Files"><div class="list-caption"><span>{{ tab === 'library' ? total : displayed.length }} {{ tab === 'library' ? $t('organizer.indexedItems') : $t('organizer.items') }}<span v-if="query && tab === 'files'">{{ $t('organizer.of') }}{{ files.length }}</span></span><span>{{ selection.size ? `${selection.size} selected` : $t('organizer.spaceToPreviewShiftToSelectARange') }}</span></div><div class="table-heading"><input type="checkbox" :checked="allSelected" :indeterminate="selection.size > 0 && !allSelected" :disabled="!displayed.length" aria-label="Select all visible results" @change="selectAll"/><button type="button" class="sort-heading" @click="setSort('name')">{{ $t('organizer.name') }}<i v-if="sort === 'name'" class="material-symbols" :textContent="direction === 'asc' ? 'arrow_upward' : 'arrow_downward'" aria-hidden="true"></i></button><button type="button" class="sort-heading" @click="setSort('modified')">{{ $t('organizer.modified') }}<i v-if="sort === 'modified'" class="material-symbols" :textContent="direction === 'asc' ? 'arrow_upward' : 'arrow_downward'" aria-hidden="true"></i></button><button type="button" class="sort-heading" @click="setSort('size')">{{ $t('organizer.size') }}<i v-if="sort === 'size'" class="material-symbols" :textContent="direction === 'asc' ? 'arrow_upward' : 'arrow_downward'" aria-hidden="true"></i></button><button type="button" class="sort-heading" @click="setSort(tab === 'library' && !['name','modified','size','type'].includes(sort) ? sort : 'type')">{{ tab === 'library' && !['name','modified','size','type'].includes(sort) ? sortLabel : $t('organizer.type') }}<i v-if="sort === 'type' || (tab === 'library' && !['name','modified','size','type'].includes(sort))" class="material-symbols" :textContent="direction === 'asc' ? 'arrow_upward' : 'arrow_downward'" aria-hidden="true"></i></button></div>
              <div ref="listElement" class="virtual-list" role="listbox" aria-label="File list" aria-multiselectable="true" tabindex="0" :aria-activedescendant="focused >= 0 ? `organizer-row-${focused}` : undefined" @scroll="onScroll" @keydown="listKeys"><div :style="{ height: `${windowRows.before}px` }"></div><div v-for="(file, offset) in rendered" :id="`organizer-row-${windowRows.start + offset}`" :key="itemKey(file)" class="file-row" :class="{ selected: selection.has(itemKey(file)), focused: focused === windowRows.start + offset }" :style="{ height: `${rowHeight}px` }" role="option" tabindex="-1" :aria-selected="selection.has(itemKey(file))" @click="selectFile(file, windowRows.start + offset, $event)" @dblclick="openFile(file)"><input type="checkbox" :checked="selection.has(itemKey(file))" :aria-label="`Select ${file.name}`" tabindex="-1" @click.stop="selectFile(file, windowRows.start + offset, { ctrlKey: true, shiftKey: $event.shiftKey })"/><span class="file-name"><span class="file-icon" :class="fileKind(file)"><i class="material-symbols">{{ fileIcon(file) }}</i></span><span class="name-lines"><strong>{{ file.name }}</strong><small v-if="tab === 'library'">{{ file.path }}</small><small v-if="tab === 'library' && !['name','modified','size','type'].includes(sort)" class="metadata-line">{{ sortLabel }}{{ $t('organizer.separator5') }}{{ lastColumn(file) }}</small></span></span><span class="file-date">{{ formatDate(file.modified) }}</span><span class="file-size">{{ isFolder(file) ? $t('organizer.separator6') : formatBytes(file.size) }}</span><span class="file-type">{{ lastColumn(file) }}</span></div><div :style="{ height: `${windowRows.after}px` }"></div><div v-if="busy && !displayed.length" class="empty-state"><span class="loading-ring"></span><h2>{{ $t('organizer.makingRoom') }}</h2><p>{{ $t('organizer.loadingYourFiles') }}</p></div><div v-else-if="!displayed.length" class="empty-state"><i class="material-symbols" :textContent="query ? 'search' : 'folder_open'" aria-hidden="true"></i><h2>{{ query ? $t('organizer.nothingMatchesJustYet') : $t('organizer.aClearSpace') }}</h2><p>{{ query ? $t('organizer.tryAnotherNameOrADifferentType') : tab === 'library' ? $t('organizer.indexThisLocationToExploreItsLibrary') : $t('organizer.yourFilesWillAppearHereUploadSomethingToBegin') }}</p></div></div>
              <div v-if="tab === 'library' && total > pageSize" class="pagination"><button type="button" :disabled="offset === 0 || busy" @click="offset = Math.max(0, offset - pageSize)">{{ $t('organizer.previous') }}</button><span>{{ offset + 1 }}{{ $t('organizer.separator7') }}{{ Math.min(offset + pageSize, total) }}{{ $t('organizer.of') }}{{ total }}</span><button type="button" :disabled="offset + pageSize >= total || busy" @click="offset += pageSize">{{ $t('organizer.next') }}</button></div>
            </section>
            <aside v-if="preview" class="detail-panel" aria-label="Quick preview"><div class="panel-heading"><span>{{ $t('organizer.quickLook') }}</span><button type="button" aria-label="Close preview" @click="closePreview"><i class="material-symbols">close</i></button></div><img v-if="previewImage" class="preview-image" :src="previewImage" :alt="preview.name"/><video v-else-if="previewURL && preview.type?.startsWith('video')" class="preview-media" :src="previewURL" controls preload="none" :aria-label="preview.name"></video><audio v-else-if="previewURL && preview.type?.startsWith('audio')" class="preview-media" :src="previewURL" controls preload="none" :aria-label="preview.name"></audio><iframe v-else-if="previewURL && preview.type === 'application/pdf'" class="preview-pdf" :src="previewURL" :title="preview.name"></iframe><div v-else-if="previewText !== ''" class="preview-text-wrap"><pre class="preview-text">{{ previewText }}</pre><p v-if="previewTruncated" class="preview-hint">{{ $t('organizer.first64KbShownOpenTheFullViewerForMore') }}</p></div><div v-else class="preview-placeholder"><i class="material-symbols">{{ fileIcon(preview) }}</i><span>{{ previewLoading ? $t('organizer.loadingPreview') : $t('organizer.openTheFullViewerForThisFormat') }}</span></div><p v-if="previewError" class="preview-hint" role="status">{{ previewError }}</p><h2>{{ preview.name }}</h2><p class="detail-path">{{ preview.source }}{{ $t('organizer.separator3') }}{{ preview.path }}</p><dl><dt>{{ $t('organizer.size') }}</dt><dd>{{ isFolder(preview) ? $t('organizer.folder') : formatBytes(preview.size) }}</dd><dt>{{ $t('organizer.modified') }}</dt><dd>{{ formatDate(preview.modified, true) }}</dd><template v-for="(value, field) in preview.metadata || {}" :key="field"><dt>{{ field }}</dt><dd>{{ value ?? '—' }}</dd></template></dl><button type="button" class="primary full-width" @click="openFile(preview)"><i class="material-symbols">open_in_new</i>{{ isFolder(preview) ? $t('organizer.openFolder') : $t('organizer.openFullViewer') }}</button><form v-if="tab === 'library' && canTag" class="tag-form" @submit.prevent="saveTags"><label for="workspace-tags">{{ $t('organizer.tags2') }}<small>{{ $t('organizer.separatedByCommas') }}</small></label><input id="workspace-tags" v-model="tagInput" placeholder="e.g. family, to organize"/><button type="submit" :disabled="busy">{{ $t('organizer.saveTags') }}</button></form></aside>
            <aside v-else-if="split" class="detail-panel destination-pane" aria-label="Destination folder"><div class="panel-heading"><span>{{ $t('organizer.destination') }}</span><button type="button" aria-label="Close destination pane" @click="split = false"><i class="material-symbols">close</i></button></div><p>{{ $t('organizer.browseASecondFolderThenMoveOrCopyYourSelection') }}</p><select v-model="destination.source" aria-label="Destination source" @change="loadDestination('/')"><option v-for="name in sources" :key="name">{{ name }}</option></select><form @submit.prevent="loadDestination(destination.path)"><input v-model="destination.path" aria-label="Destination path"/><button type="submit" aria-label="Browse destination"><i class="material-symbols">arrow_forward</i></button></form><button type="button" :disabled="destination.path === '/'" @click="loadDestination(getParentDir(destination.path))"><i class="material-symbols">arrow_upward</i>{{ $t('organizer.parentFolder') }}</button><button type="button" v-for="folder in destinationFolders" :key="folder.path" class="destination-folder" @click="loadDestination(folder.path)"><i class="material-symbols">folder</i>{{ folder.name }}<i class="material-symbols">chevron_right</i></button><p v-if="!destinationFolders.length">{{ $t('organizer.noSubfolders') }}</p><div class="destination-actions"><button type="button" :disabled="!selection.size || !canMoveHere" @click="confirmOperation('move')">{{ $t('organizer.moveHere') }}</button><button type="button" :disabled="!selection.size || !canCopyHere" @click="confirmOperation('copy')">{{ $t('organizer.copyHere') }}</button></div></aside>
          </div>
        </template>

        <section v-else-if="tab === 'activity'" class="activity-list" aria-label="Background jobs"><div class="section-heading"><h2>{{ $t('organizer.everyOperationAccountedFor') }}</h2><button type="button" @click="refresh()"><i class="material-symbols">refresh</i>{{ $t('organizer.refresh') }}</button></div><p class="section-note">{{ $t('organizer.movesCopiesAndTrashOperationsContinueOnTheServerWhen') }}</p><article v-for="job in jobs" :key="job.id" class="job-card"><div class="job-heading"><span class="job-symbol"><i class="material-symbols" :textContent="job.kind === 'trash' ? 'delete' : job.kind === 'copy' ? 'content_copy' : 'drive_file_move'" aria-hidden="true"></i></span><div><h3>{{ job.kind }} <span>{{ $t('organizer.separator8') }}{{ job.total }}{{ $t('organizer.items2') }}</span></h3><p>{{ formatDate(job.createdAt, true) }}</p></div><span class="status-pill" :class="job.status">{{ job.status }}</span></div><progress :value="job.completed + job.failed" :max="job.total || 1" :aria-label="`${job.kind} progress`"></progress><div class="job-footer"><span>{{ job.completed }}{{ $t('organizer.completed') }}<span v-if="job.failed">{{ $t('organizer.separator3') }}{{ job.failed }}{{ $t('organizer.failed') }}</span></span><button type="button" v-if="['queued','running'].includes(job.status)" @click="jobAction(job, 'cancel')">{{ $t('organizer.stopPending') }}</button><button type="button" v-if="job.failed > 0" @click="jobAction(job, 'retry')">{{ $t('organizer.retryFailedItems') }}</button></div><details v-if="job.items?.some(item => item.error)"><summary>{{ $t('organizer.reviewFailures') }}</summary><p v-for="item in job.items.filter(item => item.error)" :key="itemKey(item)" class="failure-line">{{ item.path }}{{ $t('organizer.separator5') }}{{ item.error }}</p></details></article><div v-if="!jobs.length && !busy" class="empty-state"><i class="material-symbols">check_circle</i><h2>{{ $t('organizer.allQuietHere') }}</h2><p>{{ $t('organizer.yourOrganizingActivityWillAppearHere') }}</p></div></section>
        <section v-else class="trash-list" aria-label="Recoverable trash"><div class="section-heading"><h2>{{ $t('organizer.aSecondChanceForYourFiles') }}</h2><button type="button" @click="refresh()"><i class="material-symbols">refresh</i>{{ $t('organizer.refresh') }}</button></div><p class="section-note">{{ $t('organizer.trashedItemsAreRetainedInPrivateRecoverableStorageUntilYou') }}</p><article v-for="item in trash" :key="item.id" class="trash-row"><span class="file-icon"><i class="material-symbols">{{ fileIcon(item) }}</i></span><div><strong>{{ item.name }}</strong><small>{{ item.source }}{{ $t('organizer.separator3') }}{{ item.path }}</small><small v-if="item.status !== 'ready'" class="trash-review">{{ $t('organizer.needsReviewRecoveryIsIncompleteRestoreIsDisabledToProtect') }}</small></div><span>{{ formatDate(item.deletedAt) }}</span><button type="button" :disabled="busy || item.status !== 'ready' || !sourcePermission(item.source, 'create')" @click="confirmRestore(item)"><i class="material-symbols">restore</i>{{ $t('organizer.restore') }}</button></article><div v-if="!trash.length && !busy" class="empty-state"><i class="material-symbols">delete</i><h2>{{ $t('organizer.nothingLeftBehind') }}</h2><p>{{ $t('organizer.filesYouMoveToTrashWillBeRecoverableHere') }}</p></div></section>
      </section>
      <div v-if="selection.size && ['files','library'].includes(tab)" class="selection-bar" role="region" aria-label="Selection actions"><button type="button" class="clear-selection" aria-label="Clear selection" @click="clearSelection"><i class="material-symbols">close</i></button><strong>{{ selection.size }}{{ $t('organizer.selected') }}</strong><button type="button" v-if="selection.size === 1" title="Quick preview" aria-label="Quick preview selected file" @click="showPreview(selectedFiles[0])"><i class="material-symbols">visibility</i></button><span class="selection-divider"></span><button type="button" :disabled="!canMove" @click="beginOperation('move')"><i class="material-symbols">drive_file_move</i>{{ $t('organizer.move') }}</button><button type="button" :disabled="!canCopy" @click="beginOperation('copy')"><i class="material-symbols">content_copy</i>{{ $t('organizer.copy') }}</button><button type="button" :disabled="!canDownload" @click="downloadSelection"><i class="material-symbols">download</i>{{ $t('organizer.download') }}</button><button type="button" :disabled="!canDelete" @click="confirmOperation('trash')"><i class="material-symbols">delete</i>{{ $t('organizer.trash') }}</button></div>
    </main>
    <dialog ref="operationDialog" class="operation-dialog" @cancel.prevent="closeOperation"><form @submit.prevent="submitOperation"><div class="dialog-icon"><i class="material-symbols" :textContent="operation === 'trash' ? 'delete' : operation === 'restore' ? 'restore' : 'drive_file_move'" aria-hidden="true"></i></div><h2>{{ operationTitle }}</h2><p v-if="operation === 'trash'">{{ $t('organizer.theseItemsWillMoveToRecoverableTrashIncludingEverythingInside') }}</p><p v-else-if="operation === 'restore'">{{ $t('organizer.thisReturnsTheItemToItsOriginalLocationIfThat') }}</p><p v-else>{{ $t('organizer.chooseADestinationForThese') }}{{ operationItems.length }}{{ $t('organizer.itemsExistingFilesWillNeverBeOverwritten') }}</p><div v-if="['move','copy'].includes(operation)" class="dialog-destination"><label>{{ $t('organizer.destinationSource') }}<select v-model="destination.source"><option v-for="name in sources" :key="name">{{ name }}</option></select></label><label>{{ $t('organizer.destinationFolder') }}<input v-model="destination.path" required placeholder="/"/></label><button type="button" @click="browseFromDialog">{{ $t('organizer.browseFolders') }}</button></div><p v-if="operationItems.length > 2000" class="dialog-error" role="alert">{{ $t('organizer.aJobSupportsUpTo2000SelectedItemsSelect') }}</p><ul class="operation-summary"><li v-for="item in operationItems.slice(0, 8)" :key="itemKey(item)">{{ item.name || item.path }}<small>{{ item.source }}{{ $t('organizer.separator3') }}{{ item.path }}</small></li><li v-if="operationItems.length > 8">{{ $t('organizer.and') }}{{ operationItems.length - 8 }}{{ $t('organizer.more') }}</li></ul><p class="dialog-error" v-if="operationError" role="alert">{{ operationError }}</p><div class="dialog-actions"><button type="button" :disabled="submitting" @click="closeOperation">{{ $t('organizer.cancel') }}</button><button class="primary" type="submit" :disabled="submitting || operationItems.length > 2000">{{ submitting ? $t('organizer.starting') : `${operation === 'trash' ? 'Move to trash' : operation === 'restore' ? 'Restore' : operation === 'copy' ? 'Copy' : 'Move'} ${operationItems.length} ${operationItems.length === 1 ? 'item' : 'items'}` }}</button></div></form></dialog>
    <Prompts/><Notifications/><Toast :toasts="toasts"/>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { resourcesApi, settingsApi, mediaApi } from '@/api';
import { organizerApi } from '@/api/organizer';
import { fetchURL } from '@/api/utils';
import { ensureViewToken, setActiveViewGrantScope } from '@/api/viewToken';
import { state, mutations, getters } from '@/store';
import { events, notify } from '@/notify';
import { generateRandomCode } from '@/utils/auth';
import { buildItemUrl, getParentDir, goToItem } from '@/utils/url';
import { uploadManager } from '@/utils/upload';
import { downloadManager } from '@/utils/downloadManager';
import { duplicateReviewSelection, favoritesKey, formatBytes, isFolder, itemKey, normalizePath, organizerFilterCount, readPreviewText, selectRange, virtualWindow, visibleItems } from '@/utils/organizer';
import Prompts from '@/components/prompts/Prompts.vue';
import Notifications from '@/components/Notifications.vue';
import Toast from '@/components/Toast.vue';

const route = useRoute(), router = useRouter();
const sections = [{ id: 'files', name: 'My files', icon: 'folder_open' }, { id: 'library', name: 'Library', icon: 'grid_view' }, { id: 'activity', name: 'Activity', icon: 'history' }, { id: 'trash', name: 'Trash', icon: 'delete' }];
const tab = ref(sections.some(s => s.id === route.query.tab) ? route.query.tab : 'files');
const source = ref(String(route.query.source || state.sources.current || ''));
const path = ref(normalizePath(route.query.path || '/')), address = ref(path.value);
const files = ref([]), libraryItems = ref([]), jobs = ref([]), trash = ref([]), duplicates = ref([]), scan = ref(null);
const query = ref(''), category = ref(''), sort = ref('name'), direction = ref('asc'), density = ref('comfortable');
const showFilters = ref(false), minSize = ref(''), maxSize = ref(''), modifiedAfter = ref(''), modifiedBefore = ref('');
const busy = ref(false), error = ref(''), notice = ref(''), toasts = ref([]), favorites = ref([]);
const selection = ref(new Set()), anchor = ref(-1), focused = ref(-1), selectedFiles = ref([]);
const listElement = ref(null), addressInput = ref(null), scrollTop = ref(0), viewportHeight = ref(640);
const split = ref(false), destination = ref({ source: source.value, path: '/' }), destinationFolders = ref([]);
const preview = ref(null), previewImage = ref(''), previewLoading = ref(false), tagInput = ref('');
const previewURL = ref(''), previewText = ref(''), previewTruncated = ref(false), previewError = ref('');
const showDuplicates = ref(false), keepers = ref({}), duplicateSnapshots = new Map();
const offset = ref(0), total = ref(0), pageSize = 250;
const operationDialog = ref(null), operation = ref(''), operationItems = ref([]), operationError = ref(''), submitting = ref(false);
const directoryCache = new Map();
let requestSerial = 0, queryTimer, pollTimer, resizeObserver, previewController, disposed = false;

const sources = computed(() => {
  const scoped = new Set((state.user.scopes || []).map(s => s.name).filter(Boolean));
  return Object.keys(state.sources.info || {}).filter(name => scoped.has(name));
});
const sourcePermission = (name, permission) => {
  const permissions = getters.sourcePermissions(name);
  return !!permissions?.view && !!permissions?.[permission] && (!['create','modify','delete'].includes(permission) || !state.sources.info[name]?.readOnly);
};
const selectionPermission = permission => (selectedFiles.value.length ? selectedFiles.value : [{ source: source.value }]).every(file => sourcePermission(file.source, permission));
const canCreate = computed(() => sourcePermission(source.value, 'create'));
const canDelete = computed(() => selectionPermission('delete'));
const canDownload = computed(() => selectionPermission('download'));
const canCopy = computed(() => selectionPermission('download') && sources.value.some(name => sourcePermission(name, 'create')));
const canMoveHere = computed(() => selectionPermission('modify') && sourcePermission(destination.value.source, 'create'));
const canCopyHere = computed(() => selectionPermission('download') && sourcePermission(destination.value.source, 'create'));
const canMove = computed(() => selectionPermission('modify') && sources.value.some(name => sourcePermission(name, 'create')));
const canTag = computed(() => !!preview.value && sourcePermission(preview.value.source, 'modify'));
const userInitial = computed(() => (state.user.username || 'U').slice(0, 1).toUpperCase());
const runningJobs = computed(() => jobs.value.filter(j => ['queued','running'].includes(j.status)).length);
const transferCount = computed(() => uploadManager.queue.filter(j => !['completed','cancelled','error'].includes(j.status)).length + downloadManager.queue.filter(j => !['completed','cancelled','error'].includes(j.status)).length);
const scanRunning = computed(() => ['queued','running'].includes(scan.value?.status));
const pageTitle = computed(() => tab.value === 'files' ? path.value === '/' ? 'My files' : path.value.split('/').filter(Boolean).pop() : sections.find(s => s.id === tab.value)?.name);
const sectionSubtitle = computed(() => ({ files: 'LESS CLUTTER. MORE CLARITY.', library: 'SEE THE BIGGER PICTURE.', activity: 'A PLACE FOR PROGRESS.', trash: 'ROOM TO CHANGE YOUR MIND.' }[tab.value]));
const sectionDescription = computed(() => ({ files: 'Find it, make space for it, put it where it belongs.', library: 'One view across your folders. Find the details that matter.', activity: 'Keep an eye on the work happening in the background.', trash: 'Review what you’ve set aside. Bring it back when you need it.' }[tab.value]));
const displayed = computed(() => tab.value === 'library' ? libraryItems.value : visibleItems(files.value, query.value, category.value, sort.value, direction.value, { minSize: minSize.value, maxSize: maxSize.value, modifiedAfter: modifiedAfter.value, modifiedBefore: modifiedBefore.value }));
const activeFilterCount = computed(() => organizerFilterCount(query.value, category.value, { minSize: minSize.value, maxSize: maxSize.value, modifiedAfter: modifiedAfter.value, modifiedBefore: modifiedBefore.value }));
const rowHeight = computed(() => density.value === 'compact' ? 46 : 64);
const windowRows = computed(() => virtualWindow(displayed.value.length, scrollTop.value, viewportHeight.value, rowHeight.value));
const rendered = computed(() => displayed.value.slice(windowRows.value.start, windowRows.value.end));
const allSelected = computed(() => displayed.value.length > 0 && displayed.value.every(f => selection.value.has(itemKey(f))));
const isPinned = computed(() => favorites.value.some(f => f.source === source.value && f.path === path.value));
const sortLabel = computed(() => ({ capturedAt: 'Capture date', width: 'Width', height: 'Height', duration: 'Duration', artist: 'Artist', album: 'Album', title: 'Title', tags: 'Tags' }[sort.value] || 'Type'));
const operationTitle = computed(() => `${operation.value === 'trash' ? 'Move to trash' : operation.value === 'restore' ? 'Restore' : operation.value === 'copy' ? 'Copy' : 'Move'} ${operationItems.value.length} ${operationItems.value.length === 1 ? 'item' : 'items'}?`);

function readFavorites() {
  const key = favoritesKey(state.user);
  try { const value = key ? JSON.parse(localStorage.getItem(key) || '[]') : []; favorites.value = Array.isArray(value) ? value.filter(f => f && sources.value.includes(f.source) && typeof f.path === 'string' && typeof f.name === 'string') : []; } catch { favorites.value = []; }
}
function toggleFavorite() {
  const key = favoritesKey(state.user);
  if (!key || !source.value) return;
  favorites.value = isPinned.value ? favorites.value.filter(f => !(f.source === source.value && f.path === path.value)) : [...favorites.value, { source: source.value, path: path.value, name: path.value === '/' ? source.value : pageTitle.value }];
  try { localStorage.setItem(key, JSON.stringify(favorites.value)); } catch { error.value = 'Your browser could not save pinned locations.'; }
}
function syncRoute(replace = false) { void router[replace ? 'replace' : 'push']({ query: { tab: tab.value, source: source.value || undefined, path: path.value } }); }
function clearFilters() { query.value = ''; category.value = ''; minSize.value = ''; maxSize.value = ''; modifiedAfter.value = ''; modifiedBefore.value = ''; }
function setSort(key) { if (sort.value === key) direction.value = direction.value === 'asc' ? 'desc' : 'asc'; else { sort.value = key; direction.value = 'asc'; } }
function clearSelection() { selection.value = new Set(); selectedFiles.value = []; anchor.value = -1; focused.value = -1; duplicateSnapshots.clear(); }
function resetScroll() { scrollTop.value = 0; if (listElement.value) listElement.value.scrollTop = 0; }
async function navigate(nextSource, nextPath) {
  source.value = nextSource; path.value = normalizePath(nextPath); address.value = path.value;
  tab.value = 'files'; clearFilters(); files.value = []; libraryItems.value = []; clearSelection(); closePreview(); resetScroll(); syncRoute(); await refresh();
}
async function changeSource() { path.value = '/'; address.value = '/'; clearFilters(); files.value = []; libraryItems.value = []; scan.value = null; duplicates.value = []; clearSelection(); closePreview(); syncRoute(); await refresh(); }
async function switchTab(value) { tab.value = value; clearFilters(); sort.value = 'name'; offset.value = 0; clearSelection(); closePreview(); resetScroll(); syncRoute(); await refresh(); }
async function refresh(force = false, quiet = false) {
  const serial = ++requestSerial, currentTab = tab.value;
  if (!quiet) { busy.value = true; error.value = ''; }
  try {
    if (currentTab === 'activity') { const response = await organizerApi.jobs(); if (serial === requestSerial) jobs.value = Array.isArray(response) ? response : response.jobs || []; }
    else if (currentTab === 'trash') { const response = await organizerApi.trash(); if (serial === requestSerial) trash.value = Array.isArray(response) ? response : response.items || []; }
    else if (source.value && currentTab === 'library') {
      const data = await organizerApi.library({ source: source.value, path: path.value, q: query.value, sort: sort.value, direction: direction.value, offset: String(offset.value), limit: String(pageSize) });
      if (serial === requestSerial) { libraryItems.value = data.items || []; total.value = data.total || 0; scan.value = data.scan; duplicates.value = data.duplicates || []; }
    } else if (source.value) {
      const key = itemKey({ source: source.value, path: path.value });
      const cached = directoryCache.get(key);
      if (!force && cached && Date.now() - cached.at < 15000) {
        files.value = cached.data.items || []; state.req = cached.data; state.sources.current = source.value;
      } else {
        const data = await resourcesApi.fetchFiles(source.value, path.value, false, false);
        if (serial === requestSerial) { files.value = data.items || []; state.req = data; state.sources.current = source.value; if (directoryCache.size >= 12) directoryCache.delete(directoryCache.keys().next().value); directoryCache.set(key, { at: Date.now(), data }); }
      }
    }
  } catch (e) { if (serial === requestSerial) { error.value = e.message || 'The workspace could not load this location.'; if (!quiet) { files.value = []; libraryItems.value = []; } } }
  finally { if (serial === requestSerial) busy.value = false; }
}
function onScroll(event) { scrollTop.value = event.target.scrollTop; viewportHeight.value = event.target.clientHeight; }
function selectFile(file, index, event) {
  selection.value = selectRange(displayed.value, selection.value, anchor.value, index, { shift: event.shiftKey, toggle: event.ctrlKey || event.metaKey });
  if (!event.shiftKey) anchor.value = index;
  focused.value = index; selectedFiles.value = displayed.value.filter(f => selection.value.has(itemKey(f)));
  listElement.value?.focus({ preventScroll: true });
}
function selectAll() { if (allSelected.value) clearSelection(); else { selection.value = new Set(displayed.value.map(itemKey)); selectedFiles.value = displayed.value.slice(); } }
function listKeys(event) {
  if (event.target !== listElement.value) return;
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp' || event.key === 'Home' || event.key === 'End') {
    event.preventDefault();
    const index = event.key === 'Home' ? 0 : event.key === 'End' ? displayed.value.length - 1 : Math.max(0, Math.min(displayed.value.length - 1, focused.value + (event.key === 'ArrowDown' ? 1 : -1)));
    if (index < 0 || !displayed.value[index]) return;
    if (anchor.value < 0) anchor.value = index;
    selectFile(displayed.value[index], index, event);
    const top = index * rowHeight.value;
    if (top < listElement.value.scrollTop) listElement.value.scrollTop = top;
    else if (top + rowHeight.value > listElement.value.scrollTop + listElement.value.clientHeight) listElement.value.scrollTop = top + rowHeight.value - listElement.value.clientHeight;
  } else if (event.key === 'Enter' && displayed.value[focused.value]) { event.preventDefault(); openFile(displayed.value[focused.value]); }
  else if (event.key === ' ' && displayed.value[focused.value]) { event.preventDefault(); void showPreview(displayed.value[focused.value]); }
  else if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'a') { event.preventDefault(); selection.value = new Set(displayed.value.map(itemKey)); selectedFiles.value = displayed.value.slice(); }
}
function onKeys(event) {
  if (operationDialog.value?.open) return;
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'l' && ['files','library'].includes(tab.value)) { event.preventDefault(); addressInput.value?.focus(); addressInput.value?.select(); }
  if (event.key === 'Escape') { closePreview(); clearSelection(); }
  if (event.key === '/' && !['INPUT','TEXTAREA','SELECT'].includes(event.target.tagName)) { event.preventDefault(); document.querySelector('.filter-input input')?.focus(); }
}
function fileKind(file) { return isFolder(file) ? 'folder' : (file.type || '').split('/')[0]; }
function fileIcon(file) { return isFolder(file) ? 'folder' : file.type?.startsWith('image') ? 'image' : file.type?.startsWith('video') ? 'movie' : file.type?.startsWith('audio') ? 'music_note' : 'description'; }
function formatDate(value, full = false) { if (!value || Number.isNaN(Date.parse(value))) return '—'; return new Date(value).toLocaleString(undefined, full ? { dateStyle: 'medium', timeStyle: 'short' } : { dateStyle: 'medium' }); }
function lastColumn(file) { if (tab.value === 'library' && sort.value === 'tags') return (file.tags || []).join(', ') || '—'; if (tab.value === 'library' && !['name','modified','size','type'].includes(sort.value)) { const value = file.metadata?.[sort.value]; return value === undefined || value === null || value === '' ? '—' : sort.value === 'capturedAt' ? formatDate(value) : sort.value === 'duration' ? `${Math.round(value)} sec` : String(value); } return isFolder(file) ? 'Folder' : file.type?.split('/').pop() || file.name.split('.').pop() || 'File'; }
function openFile(file) { if (isFolder(file)) void navigate(file.source, file.path); else goToItem(file.source, file.path, { source: source.value, path: path.value, name: pageTitle.value }); }
function closePreview() {
  previewController?.abort();
  if (previewImage.value) URL.revokeObjectURL(previewImage.value);
  previewImage.value = ''; previewURL.value = ''; previewText.value = ''; previewTruncated.value = false; previewError.value = ''; previewLoading.value = false; preview.value = null;
  setActiveViewGrantScope('');
}
async function showPreview(file) {
  closePreview(); preview.value = file; tagInput.value = (file.tags || []).join(', '); split.value = false;
  if (isFolder(file) || !sourcePermission(file.source, 'view')) return;
  previewController = new AbortController(); const controller = previewController; previewLoading.value = true;
  try {
    if (file.type?.startsWith('image')) {
      const image = await resourcesApi.fetchPreviewImage(resourcesApi.getPreviewURL(file.source, file.path, file.modified), controller.signal);
      if (!controller.signal.aborted) previewImage.value = image; else URL.revokeObjectURL(image);
    } else {
      setActiveViewGrantScope(file.source);
      const token = await ensureViewToken(file.source);
      if (controller.signal.aborted) return;
      if (file.type?.startsWith('video') || file.type?.startsWith('audio')) previewURL.value = mediaApi.getStreamURL(file.source, file.path, token);
      else if (file.type === 'application/pdf') previewURL.value = resourcesApi.getRawViewURL(file.source, file.path, token);
      else if (file.type?.startsWith('text') || ['application/json','application/xml','application/javascript'].includes(file.type)) {
        const response = await fetchURL(resourcesApi.getRawViewURL(file.source, file.path, token), { headers: { Range: 'bytes=0-65535' }, signal: controller.signal });
        const result = await readPreviewText(response);
        if (!controller.signal.aborted) { previewText.value = result.text; previewTruncated.value = result.truncated; }
      }
    }
  }
  catch (e) { if (e.name !== 'AbortError' && !controller.signal.aborted) previewError.value = e.message || 'The quick preview is unavailable. Try the full viewer.'; }
  finally { if (previewController === controller) previewLoading.value = false; }
}
async function saveTags() { busy.value = true; try { const tags = [...new Set(tagInput.value.split(',').map(t => t.trim()).filter(Boolean))]; await organizerApi.tags({ source: preview.value.source, path: preview.value.path, tags }); preview.value.tags = tags; notice.value = 'Tags saved.'; await refresh(); } catch(e) { error.value = e.message; } finally { busy.value = false; } }
function upload() { state.req = { ...state.req, source: source.value, path: path.value, items: files.value }; mutations.showPrompt({ name: 'upload', props: { targetPath: path.value } }); }
function openTransfers() { state.req = { ...state.req, source: source.value, path: path.value, items: tab.value === 'files' ? files.value : [] }; mutations.showPrompt({ name: downloadManager.queue.length && !uploadManager.queue.some(j => ['pending','uploading','paused'].includes(j.status)) ? 'download' : 'upload', props: { targetPath: path.value } }); }
async function downloadSelection() { try { const grouped = new Map(); for (const file of selectedFiles.value) { if (!grouped.has(file.source)) grouped.set(file.source, []); grouped.get(file.source).push({ ...file, isDir: isFolder(file) }); } for (const items of grouped.values()) await resourcesApi.download('zip', items); notice.value = 'Download requested. Your browser will show its progress.'; } catch(e) { error.value = e.message; } }
async function toggleSplit() { split.value = !split.value; if (split.value) { closePreview(); destination.value = { source: source.value, path: path.value }; await loadDestination(path.value); } }
async function loadDestination(nextPath) { try { const target = normalizePath(nextPath); const data = await resourcesApi.fetchFiles(destination.value.source, target, false, false); destination.value.path = target; destinationFolders.value = (data.items || []).filter(isFolder); } catch(e) { error.value = e.message; } }
function beginOperation(kind) { if (!split.value) destination.value = { source: source.value, path: path.value }; confirmOperation(kind); }
function confirmOperation(kind) { operation.value = kind; operationItems.value = selectedFiles.value.map(f => ({ ...f })); operationError.value = ''; if (!operationItems.value.length) return; operationDialog.value.showModal(); }
function confirmRestore(item) { operation.value = 'restore'; operationItems.value = [{ ...item, trashId: item.id }]; operationError.value = ''; operationDialog.value.showModal(); }
function closeOperation() { if (!submitting.value) operationDialog.value?.close(); }
async function browseFromDialog() { operationDialog.value.close(); split.value = true; closePreview(); await loadDestination(destination.value.path); }
async function submitOperation() {
  submitting.value = true; operationError.value = '';
  try {
    if (operationItems.value.length > 2000) throw new Error('Select at most 2,000 items per job, or select their parent folders.');
    // The server rehashes each duplicate and keeper at execution, including after
    // queued work resumes. Directory response sizes may be allocated disk bytes.
    const items = operationItems.value.map(f => { const snapshot = duplicateSnapshots.get(itemKey(f)); return { source: f.source, path: f.path, ...(f.trashId && { trashId: f.trashId }), ...(snapshot && { expectedSha256: snapshot.sha256, keeper: snapshot.keeper }) }; });
    const body = { kind: operation.value, items, ...(['move','copy'].includes(operation.value) && { destination: { source: destination.value.source, path: normalizePath(destination.value.path) } }) };
    await organizerApi.createJob(body); operationDialog.value.close(); clearSelection(); directoryCache.clear(); notice.value = 'Operation queued. Follow progress and any conflicts in Activity.'; await refresh(true);
  } catch(e) { operationError.value = e.message || 'The operation could not start.'; }
  finally { submitting.value = false; }
}
async function jobAction(job, action) { try { if (action === 'cancel') await organizerApi.cancelJob(job.id); else await organizerApi.retryJob(job.id); await refresh(); } catch(e) { error.value = e.message; } }
async function startScan() { try { scan.value = await organizerApi.scan({ source: source.value, path: path.value }); notice.value = 'Library scan started. You can leave this page while it runs.'; await refresh(); } catch(e) { error.value = e.message; } }
function selectDuplicates(group) {
  const review = duplicateReviewSelection(group, keepers.value[group.sha256] || itemKey(group.files[0]));
  if (!review.keeper) return;
  selection.value = new Set(review.copies.map(itemKey));
  selectedFiles.value = review.copies;
  duplicateSnapshots.clear();
  for (const snapshot of review.snapshots) {
    duplicateSnapshots.set(snapshot.key, { sha256: snapshot.sha256, keeper: snapshot.keeper });
  }
  notice.value = `${review.copies.length} duplicate copies selected. The keeper remains untouched. Review the selection before moving or trashing.`;
}
function chooseKeeper(group, file) { keepers.value[group.sha256] = itemKey(file); clearSelection(); notice.value = 'Keeper changed. Select the other copies again to review cleanup.'; }

watch([query, category, sort, direction, minSize, maxSize, modifiedAfter, modifiedBefore], () => { clearTimeout(queryTimer); clearSelection(); resetScroll(); if (tab.value === 'library') { offset.value = 0; queryTimer = setTimeout(() => { void refresh(); }, 250); } });
watch(offset, () => { clearSelection(); resetScroll(); if (tab.value === 'library') void refresh(); });
watch(() => state.user.id ?? state.user.username, () => { directoryCache.clear(); clearSelection(); readFavorites(); });
watch(() => route.query, next => { const nextTab = sections.some(s => s.id === next.tab) ? next.tab : 'files'; const nextSource = String(next.source || source.value); const nextPath = normalizePath(next.path || '/'); if (nextTab === tab.value && nextSource === source.value && nextPath === path.value) return; tab.value = nextTab; source.value = nextSource; path.value = nextPath; address.value = nextPath; clearFilters(); files.value = []; libraryItems.value = []; clearSelection(); closePreview(); resetScroll(); void refresh(); });
watch(() => state.reload, () => { directoryCache.clear(); void refresh(true); });
watch(listElement, element => { resizeObserver?.disconnect(); if (element) { viewportHeight.value = element.clientHeight; resizeObserver = new ResizeObserver(() => { viewportHeight.value = element.clientHeight; }); resizeObserver.observe(element); } });
onMounted(async () => {
  if (!state.sessionId) mutations.setSession(generateRandomCode(8));
  const maxUploads = state.user.fileLoading?.maxConcurrentUpload || 0;
  if (maxUploads < 1 || maxUploads > 10) mutations.setMaxConcurrentUpload(1);
  notify.setToastUpdateCallback(value => { toasts.value = value; }); toasts.value = notify.getToasts();
  try { const info = await settingsApi.sources(); if (disposed) return; mutations.updateSourceInfo(info); if (!sources.value.includes(source.value)) source.value = sources.value[0] || ''; destination.value.source = source.value; readFavorites(); if (state.user.permissions?.realtime) events.startSSE(); syncRoute(true); await refresh(); }
  catch(e) { error.value = e.message || 'Storage sources could not load.'; }
  if (disposed) return;
  pollTimer = setInterval(() => { void pollJobs(); }, 4000);
  await nextTick();
});
async function pollJobs() {
    if (document.visibilityState !== 'visible') return;
    try {
      const data = await organizerApi.jobs();
      if (disposed) return;
      const next = Array.isArray(data) ? data : data.jobs || [];
      const signature = list => JSON.stringify(list.map(job => [job.id, job.status, job.completed, job.failed]));
      const changed = signature(jobs.value) !== signature(next);
      jobs.value = next;
      if (changed) directoryCache.clear();
      if (tab.value === 'activity' || tab.value === 'library' && scanRunning.value || changed && ['files','trash'].includes(tab.value)) await refresh(changed, true);
    } catch { /* A visible refresh reports connection errors without repeated notification noise. */ }
}
onBeforeUnmount(() => { disposed = true; requestSerial++; clearInterval(pollTimer); clearTimeout(queryTimer); resizeObserver?.disconnect(); closePreview(); });
</script>

<style scoped>
.filter-toggle{display:inline-flex;align-items:center;gap:6px;white-space:nowrap}.filter-toggle.active{color:#637e50;background:#edf2e7}.filter-toggle>span{display:grid;place-items:center;min-width:17px;height:17px;border-radius:50%;background:#e4ebda;color:#607b4d;font-size:10px}.filter-panel{margin:-10px 0 18px;padding:15px 17px;border:1px solid var(--line);border-radius:9px;background:#fafbf7}.filter-panel>div{display:flex;align-items:end;gap:12px;flex-wrap:wrap}.filter-panel label{display:grid;gap:5px;color:#89947d;font-size:10px}.filter-panel input{width:150px;min-height:34px;font-size:11px}.filter-panel p{margin:10px 0 0;color:#98a18e;font-size:10px}.filter-panel .clear-filters{min-height:34px;border:1px solid var(--line);font-size:10px}.table-heading .sort-heading{display:flex;align-items:center;gap:5px;min-width:0;padding:0;border:0;background:transparent;color:inherit;text-align:left;font-size:inherit;letter-spacing:inherit}.table-heading .sort-heading:hover{color:#637e50}.sort-heading .material-symbols{font-size:13px}

.organizer .workspace-top{position:relative;inset:auto;width:100%;transform:none;z-index:auto;box-shadow:none;margin:0;background:var(--canvas)}
.organizer .workspace-nav nav{position:static;inset:auto;width:auto;height:auto;transform:none;background:transparent;border:0;border-radius:0;box-shadow:none;margin:0}
.organizer :is(.workspace-eyebrow,.nav-caption,.nav-hint,.nav-bottom,.page-description,.eyebrow,.filter-input,.list-caption,.table-heading,.file-date,.file-size,.file-type,.name-lines small,.detail-path,.panel-heading,.tag-form label,.tag-form small,.destination-pane>p,.section-note,.section-heading>span,.job-heading h3 span,.job-heading p,.job-footer,.status-pill,.trash-row small,.trash-row>span:not(.file-icon),.index-status,.duplicate-summary>span,.duplicate-file,.keeper-label,.pagination,.operation-dialog p,.operation-summary small,.dialog-destination label,.preview-hint){color:#596e5e}
.organizer .list-caption>span:first-child,.organizer .detail-panel dt,.organizer .detail-panel dl,.organizer .preview-placeholder{color:#596e5e}
.organizer .metadata-line{color:#526f43!important}
.metadata-line{color:#71855e!important;font-size:9px!important}.trash-review{color:#ac7863!important}.preview-media{width:100%;max-height:260px;border-radius:8px;background:#f1f4eb}.preview-pdf{width:100%;height:360px;border:1px solid #e7ebe6;border-radius:8px}.preview-text{white-space:pre-wrap;overflow-wrap:anywhere;overflow:auto;max-height:330px;font:11px/1.6 ui-monospace,Consolas,monospace;margin:0;background:#f6f8f1;padding:12px;border-radius:7px;color:#526447}.preview-hint{font-size:10px;line-height:1.5;color:#8d9a7b}.preview-text-wrap{min-width:0}

.organizer{--ink:#233b35;--muted:#78847f;--accent:#236b52;--line:#e7ebe6;--paper:#fff;--canvas:#f7f8f4;position:fixed;inset:0;display:flex;background:var(--canvas);color:var(--ink);font:14px/1.5 -apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;z-index:1;text-align:left;color-scheme:light}.organizer *{box-sizing:border-box}.organizer button,.organizer input,.organizer select{font:inherit}.organizer button,.organizer a{touch-action:manipulation}.organizer button{display:inline-flex;align-items:center;justify-content:center;gap:8px;border:1px solid transparent;background:transparent;color:inherit;border-radius:8px;padding:8px 11px;cursor:pointer;box-shadow:none;min-height:36px;text-transform:none;letter-spacing:normal}.organizer button:hover:not(:disabled){background:#eaf0e9}.organizer button:disabled{opacity:.38;cursor:not-allowed}.organizer :focus-visible{outline:2px solid var(--accent);outline-offset:3px}.organizer a{color:inherit;text-decoration:none}.organizer input,.organizer select{color:var(--ink);background:#fff;border:1px solid var(--line);border-radius:8px;padding:9px 12px;min-width:0;height:40px;box-shadow:none;margin:0}.organizer .material-symbols{font-size:21px;flex:none}.workspace-nav{width:224px;flex:none;border-right:1px solid var(--line);padding:32px 18px 18px;background:#f0f3ed;display:flex;flex-direction:column;overflow:auto}.workspace-brand{display:flex;align-items:center;gap:12px;font-size:23px;font-weight:650;letter-spacing:-1px;margin:0 13px 46px}.brand-mark{display:grid;place-items:center;width:34px;height:36px;color:#f7fcf4;background:var(--ink);border-radius:11px;font-family:Georgia,serif;font-size:29px;letter-spacing:-2px}.brand-light{font-weight:350}.nav-caption{font-size:10px;font-weight:700;letter-spacing:1.7px;color:#7c8c80;margin:0 13px 12px}.workspace-nav nav{display:grid;gap:5px}.workspace-nav nav button{justify-content:flex-start;gap:13px;padding:12px 14px;color:#6b7970;font-weight:500}.workspace-nav nav button.active{background:#e0e9dd;color:#2f6347}.nav-count{font-size:11px;border-radius:12px;background:#d5e3d3;padding:1px 7px;margin-left:auto}.nav-heading{display:flex;align-items:center;justify-content:space-between;margin-top:38px}.nav-heading .nav-caption{margin-bottom:0}.nav-heading button{padding:4px}.nav-hint{margin:9px 14px;color:#94a08f;font-size:12px;line-height:1.8}.favorite-link{justify-content:flex-start!important;gap:11px!important;padding:9px 14px!important;width:100%;color:#667367!important}.favorite-link span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.favorite-link i{font-size:18px!important}.nav-bottom{display:flex;align-items:center;gap:8px;margin-top:auto;padding:30px 9px 0;color:#81907e;font-size:11px}.nav-bottom a{margin-left:auto;display:flex}.connection-dot{width:6px;height:6px;background:#6b9a69;border-radius:50%}.workspace-main{flex:1;min-width:0;overflow:auto;position:relative;padding-bottom:90px}.workspace-top{height:82px;display:flex;align-items:center;justify-content:space-between;padding:0 46px;border-bottom:1px solid var(--line)}.workspace-eyebrow{color:#8d978d;font-size:12px}.top-actions{display:flex;gap:20px;align-items:center;color:#6a796c}.top-actions button{font-size:12px}.top-actions>a{display:flex}.user-avatar{width:30px;height:30px;border-radius:50%;background:#e7ebdf;display:grid;place-items:center;font-size:11px;color:#737e62}.workspace-content{max-width:1620px;margin:auto;padding:40px 46px}.page-heading{display:flex;justify-content:space-between;align-items:center;gap:20px;margin-bottom:34px}.eyebrow{font-size:9px;font-weight:700;letter-spacing:2px;color:#89977f;margin:0 0 10px}.organizer h1{font-size:36px;line-height:1.25;letter-spacing:-1.5px;font-weight:550;margin:0 0 12px;color:var(--ink)}.page-description{color:#899187;font-size:13px;margin:0}.organizer .primary{background:var(--accent);border:1px solid var(--accent);color:#fff;padding:10px 17px;white-space:nowrap;font-weight:500}.organizer .primary:hover:not(:disabled){background:#1a5943}.location-bar{display:flex;align-items:center;border:1px solid var(--line);border-radius:11px;background:var(--paper);height:50px;padding:3px 8px;margin-bottom:22px;gap:4px}.location-bar select{border:0;max-width:190px;font-size:12px;background:transparent}.location-divider{color:#c6cdc2}.address-form{display:flex;flex:1;min-width:0}.address-form input{width:100%;border:0!important;background:transparent!important;color:#788371!important;font-size:12px!important}.address-form button{color:#9aa390}.location-bar>button{color:#8b9581;padding:6px}.organizer .pinned{color:#397547!important;background:#e6efdf!important}.browser-tools{display:flex;align-items:center;gap:8px;margin-bottom:19px}.filter-input{display:flex;align-items:center;flex:1;gap:8px;border:1px solid var(--line);background:#fff;border-radius:8px;padding:0 11px;min-width:170px;color:#8f998b}.filter-input input{border:0!important;background:transparent!important;min-width:0;width:100%;padding-left:0!important;font-size:12px!important}.filter-input kbd{border:1px solid #e9ece4;border-radius:3px;font-size:10px;padding:0 5px}.browser-tools>select{font-size:11px;padding:8px 25px 8px 11px;max-width:155px}.browser-tools>button{border:1px solid var(--line);background:#fff;padding:8px}.browser-layout{display:flex;gap:20px;align-items:flex-start}.file-surface{flex:1;min-width:0;border:1px solid var(--line);background:#fff;border-radius:11px;overflow:hidden}.list-caption{height:47px;display:flex;align-items:center;justify-content:space-between;padding:0 20px;border-bottom:1px solid var(--line);font-size:11px;color:#929b8b;gap:10px}.list-caption>span:first-child{color:#5f735c;font-weight:500}.list-caption>span:last-child{font-size:10px}.table-heading,.file-row{display:grid;grid-template-columns:18px minmax(160px,1fr) 120px 85px 110px;align-items:center;gap:16px;padding:0 20px}.table-heading{height:42px;background:#fbfcf8;border-bottom:1px solid var(--line);font-size:10px;letter-spacing:.2px;color:#90998a}.table-heading input,.file-row input{accent-color:var(--accent);width:14px;height:14px;padding:0;border-radius:3px}.virtual-list{height:min(55vh,680px);min-height:250px;overflow:auto;outline:none!important;overscroll-behavior:contain}.file-row{border-bottom:1px solid #f0f2ec;font-size:11px;cursor:default;transition:background 80ms}.file-row:hover{background:#f8faf5}.file-row.selected{background:#edf4e8}.file-row.focused{box-shadow:inset 3px 0 #639276}.file-name{display:flex;align-items:center;gap:12px;min-width:0}.file-icon{width:31px;height:35px;flex:none;border-radius:6px;background:#f1f3ed;color:#9ca594;display:grid;place-items:center}.file-icon.folder{background:#eef1df;color:#91a271}.file-icon.image{background:#f5e9dc;color:#c29c6a}.file-icon.video{background:#e9e9f2;color:#a396c4}.file-icon.audio{background:#e2ecea;color:#75a29a}.file-icon .material-symbols{font-size:21px}.name-lines{min-width:0}.name-lines strong{display:block;font-size:12px;font-weight:500;color:#4b6048;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.name-lines small{display:block;font-size:10px;color:#9aa28e;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:330px}.file-date,.file-size,.file-type{color:#929b8b;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.file-type{font-size:10px}.empty-state{padding:65px 20px;text-align:center;color:#a1ad98;display:flex;flex-direction:column;align-items:center;gap:10px}.empty-state>.material-symbols{font-size:40px;color:#c0c9b6}.empty-state h2{font-size:16px;font-weight:500;margin:0;color:#677b60}.empty-state p{margin:0;max-width:300px;font-size:12px}.loading-ring{width:28px;height:28px;border:2px solid #e3eadb;border-top-color:var(--accent);border-radius:50%;animation:workspace-spin .8s linear infinite}.selection-bar{position:fixed;bottom:25px;left:calc(50% + 112px);transform:translateX(-50%);display:flex;align-items:center;gap:8px;white-space:nowrap;border-radius:12px;background:#233c32;color:#f2f5ec;padding:10px 13px;box-shadow:0 12px 45px #233c3226;z-index:4}.selection-bar strong{font-size:12px;font-weight:500;margin-right:6px}.selection-bar button{font-size:12px;padding:6px 10px}.selection-bar button:hover:not(:disabled){background:#365247}.selection-bar .clear-selection{padding:4px;color:#a4b5a4}.selection-divider{height:20px;width:1px;background:#52634f;margin:0 4px}.detail-panel{width:280px;flex:none;background:#fff;border:1px solid var(--line);border-radius:11px;padding:19px;overflow:hidden}.panel-heading{display:flex;justify-content:space-between;align-items:center;color:#8e9c83;font-size:9px;letter-spacing:1.7px;font-weight:650;margin-bottom:15px}.panel-heading button{min-height:26px;padding:3px}.detail-panel h2{font-size:16px;line-height:1.5;font-weight:550;overflow-wrap:anywhere;margin:16px 0 8px;color:#566b4e}.detail-path{font-size:11px;color:#a1a990;overflow-wrap:anywhere}.preview-image{width:100%;max-height:230px;object-fit:contain;border-radius:5px;background:#f4f6ef}.preview-placeholder{height:170px;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:15px;background:#f6f7f0;color:#a1ae93;border-radius:7px;font-size:11px;text-align:center;padding:16px}.preview-placeholder .material-symbols{font-size:52px}.detail-panel dl{display:grid;grid-template-columns:80px 1fr;gap:9px;font-size:11px;margin:20px 0;color:#74826c}.detail-panel dt{color:#a0a991;overflow-wrap:anywhere}.detail-panel dd{margin:0;overflow-wrap:anywhere}.full-width{width:100%;font-size:12px!important}.tag-form{margin-top:24px;display:grid;gap:10px}.tag-form label{color:#73816b;font-size:12px}.tag-form small{font-size:10px;color:#a0a98e}.tag-form input{width:100%;font-size:11px}.tag-form button{border:1px solid var(--line)}.destination-pane>p{font-size:11px;color:#8c9982}.destination-pane>select{width:100%;font-size:12px}.destination-pane form{display:flex;gap:4px;margin:12px 0}.destination-pane form input{width:100%;font-size:11px}.destination-pane>button{font-size:11px}.destination-folder{width:100%;justify-content:flex-start!important;border-bottom:1px solid #f0f3ea!important;text-align:left!important}.destination-folder i:last-child{margin-left:auto;color:#aeb79f}.destination-folder i:first-child{color:#9dab76}.destination-actions{display:flex;gap:8px;margin-top:20px}.destination-actions button{border:1px solid var(--line);font-size:11px;flex:1}.workspace-error{display:flex;align-items:center;gap:10px;background:#fbefea;color:#975b45;border:1px solid #f0dcd2;border-radius:8px;padding:10px 14px;margin-bottom:20px;overflow-wrap:anywhere;font-size:12px}.workspace-error button{margin-left:auto}.workspace-notice{background:#edf3e5;border:1px solid #e0e8d4;border-radius:8px;padding:11px 15px;color:#6b7e54;font-size:12px}.section-heading{display:flex;gap:20px;justify-content:space-between;align-items:center}.section-heading h2{font-size:17px;letter-spacing:-.4px;font-weight:500;margin:0}.section-heading button{font-size:11px}.section-heading>span,.section-note{font-size:12px;color:#98a18e}.section-note{margin:10px 0 24px;max-width:750px}.activity-list,.trash-list{padding:25px;background:#fff;border:1px solid var(--line);border-radius:12px}.job-card{padding:22px;border:1px solid var(--line);border-radius:9px;margin-top:15px}.job-heading{display:flex;gap:12px;align-items:center}.job-heading h3{margin:0;font-weight:550;font-size:13px;text-transform:capitalize}.job-heading h3 span{font-weight:400;color:#99a18f;text-transform:none}.job-heading p{margin:3px 0 0;font-size:10px;color:#a5ad99}.job-symbol{background:#eef3e7;border-radius:8px;color:#8ba074;padding:10px;display:flex}.status-pill{margin-left:auto;font-size:10px;padding:3px 9px;border-radius:20px;background:#f1f3ed;color:#96a087;text-transform:capitalize}.status-pill.completed{background:#e9f3e0;color:#728f55}.status-pill.failed{background:#f9ece6;color:#af8068}.job-card progress{width:100%;height:5px;accent-color:#6e9b73;margin:20px 0 5px}.job-footer{display:flex;justify-content:space-between;align-items:center;font-size:11px;color:#8d9a81}.job-footer button{font-size:11px}.job-card details{font-size:11px;color:#ac7863;margin-top:10px}.failure-line{overflow-wrap:anywhere}.trash-row{display:flex;align-items:center;gap:16px;padding:18px 0;border-bottom:1px solid var(--line);font-size:12px}.trash-row>div{flex:1;min-width:0}.trash-row strong{font-weight:500}.trash-row small{display:block;color:#a0a98f;overflow-wrap:anywhere}.trash-row>span:not(.file-icon){color:#a0a98f;font-size:11px}.trash-row button{font-size:11px}.index-status{display:flex;align-items:center;gap:9px;color:#9ca68e;font-size:11px;margin-bottom:20px}.index-status>.material-symbols{font-size:17px}.index-status button{margin-left:auto;font-size:11px;color:#658354}.duplicates-panel{background:#f1f4e9;border:1px solid #e3e8d9;border-radius:10px;padding:20px;margin-bottom:22px}.duplicates-panel .section-heading{margin-bottom:18px}.duplicates-panel .section-heading h2{font-size:15px}.duplicates-panel .section-heading>span{font-size:10px}.duplicate-group{border-top:1px solid #e0e7d6;padding:15px 0}.duplicate-summary{display:flex;gap:12px;align-items:center;font-size:11px}.duplicate-summary>i{color:#93a67e}.duplicate-summary strong{font-weight:500}.duplicate-summary>span{color:#9fa98e}.duplicate-summary button{margin-left:auto;background:#fff;font-size:11px;border:1px solid #e0e7d6}.duplicate-file{display:flex;align-items:center;gap:10px;padding:7px 0;font-size:11px;color:#8c9a7c;overflow-wrap:anywhere}.duplicate-file input{width:14px;height:14px;accent-color:var(--accent)}.keeper-label{font-size:9px;text-transform:uppercase;letter-spacing:1px;color:#6f8958}.pagination{display:flex;align-items:center;justify-content:space-between;padding:10px 16px;border-top:1px solid var(--line);font-size:11px;color:#95a086}.pagination button{font-size:11px}.operation-dialog{border:1px solid var(--line);border-radius:16px;padding:30px;background:#fcfdf9;color:var(--ink);width:min(500px,calc(100vw - 36px));box-shadow:0 25px 100px #182c3529;max-height:85vh;overflow:auto}.operation-dialog::backdrop{background:#18392f55;backdrop-filter:blur(4px)}.operation-dialog h2{font-size:24px;font-weight:550;letter-spacing:-.7px;margin:15px 0 10px}.operation-dialog p{color:#8c9982;font-size:12px}.dialog-icon{width:42px;height:42px;border-radius:11px;background:#e8efe0;color:#7d9968;display:grid;place-items:center}.dialog-destination{display:grid;gap:12px;margin:20px 0}.dialog-destination label{display:grid;gap:5px;font-size:11px;color:#8b9880}.dialog-destination input,.dialog-destination select{width:100%;font-size:12px}.dialog-destination button{justify-self:start;font-size:11px;border:1px solid var(--line)}.operation-summary{list-style:none;padding:12px 15px;border:1px solid var(--line);border-radius:8px;max-height:220px;overflow:auto;margin:22px 0}.operation-summary li{font-size:12px;color:#6a7d5d;padding:6px 0;overflow-wrap:anywhere}.operation-summary small{display:block;font-size:10px;color:#a2aa95}.dialog-actions{display:flex;justify-content:flex-end;gap:10px;margin-top:20px}.dialog-actions button{font-size:12px}.dialog-error{color:#ad6f54!important}.with-side .table-heading,.with-side .file-row{grid-template-columns:18px minmax(120px,1fr) 95px 70px;gap:11px}.with-side .file-type,.with-side .table-heading>span:last-child{display:none}@keyframes workspace-spin{to{transform:rotate(360deg)}}
@media(min-width:1600px){.workspace-content{padding-top:52px}.workspace-nav{width:248px}.selection-bar{left:calc(50% + 124px)}.workspace-top{padding-inline:60px}.workspace-content{padding-inline:60px}.virtual-list{height:min(58vh,800px)}}
@media(max-width:1150px){.workspace-nav{width:190px;padding-inline:12px}.workspace-top{padding-inline:28px}.workspace-content{padding:30px 28px}.selection-bar{left:calc(50% + 95px)}.table-heading,.file-row{grid-template-columns:18px minmax(150px,1fr) 100px 75px;gap:12px}.table-heading>span:last-child,.file-type{display:none}.detail-panel{width:245px}.with-side .table-heading,.with-side .file-row{grid-template-columns:18px minmax(100px,1fr) 65px}.with-side .file-date,.with-side .table-heading>span:nth-child(3){display:none}.browser-tools>select{max-width:125px}.browser-tools>select:last-of-type{max-width:110px}.workspace-brand{font-size:21px;margin-inline:8px}}
@media(max-width:850px){.workspace-nav{width:72px;padding:26px 10px}.workspace-brand{margin:0 auto 40px}.workspace-brand>span:last-child,.nav-caption,.nav-hint,.favorite-link,.nav-heading,.nav-bottom>span,.workspace-nav nav button:not(.active) .nav-count{display:none!important}.workspace-nav nav button{font-size:0;justify-content:center;padding:12px}.workspace-nav nav button i{font-size:23px}.workspace-nav nav button .nav-count{position:absolute;margin:0 0 24px 30px;font-size:9px}.nav-bottom{padding:20px 8px 0;justify-content:center}.nav-bottom a{margin:0}.workspace-top{height:68px}.workspace-content{padding:28px 24px}.selection-bar{left:calc(50% + 36px)}.browser-tools{flex-wrap:wrap}.filter-input{flex-basis:100%;margin-bottom:4px}.browser-tools>select{flex:1;max-width:none!important;font-size:10px}.browser-tools button{flex:none}.detail-panel{width:240px}.with-side .table-heading,.with-side .file-row{grid-template-columns:18px minmax(100px,1fr)}.with-side .file-size,.with-side .table-heading>span:nth-child(4){display:none}.list-caption>span:last-child{display:none}.workspace-eyebrow{font-size:11px}.page-heading{margin-bottom:26px}.page-description{font-size:12px;max-width:300px}.page-heading>.primary{font-size:11px;padding:9px 12px}.top-actions{gap:15px}.duplicates-panel .section-heading>span{display:none}.duplicate-summary>span{display:none}.duplicate-file{font-size:10px}}
@media(max-width:600px){.organizer{display:block}.workspace-nav{position:fixed;bottom:0;left:0;right:0;width:100%;height:62px;z-index:5;padding:4px 12px;flex-direction:row;border-right:0;border-top:1px solid var(--line);background:#f7f9f2}.workspace-brand,.nav-bottom{display:none}.workspace-nav nav{display:flex;justify-content:space-around;flex:1;gap:4px}.workspace-nav nav button{font-size:9px;display:flex;flex-direction:column;gap:0;padding:4px 16px;min-width:60px}.workspace-nav nav button i{font-size:22px}.workspace-nav nav button .nav-count{margin:0 0 24px 34px}.workspace-main{height:calc(100dvh - 62px);padding-bottom:80px}.workspace-top{padding-inline:19px;height:58px}.workspace-content{padding:26px 16px}.workspace-eyebrow{font-size:10px}.top-actions{gap:12px}.top-actions button{padding:4px;font-size:10px;gap:4px}.top-actions>a{display:none}.user-avatar{width:26px;height:26px}.organizer h1{font-size:30px}.eyebrow{font-size:8px;letter-spacing:1.4px}.page-description{font-size:11px;max-width:240px}.page-heading{align-items:flex-start;gap:12px}.page-heading>.primary{font-size:0;min-height:38px;width:40px;padding:8px;margin-top:22px;gap:0}.page-heading>.primary i{font-size:21px}.location-bar{margin-bottom:16px}.location-bar select{max-width:105px;padding-inline:4px;font-size:10px}.location-bar>button{padding:3px}.location-bar>button .material-symbols{font-size:18px}.address-form input{padding-inline:4px!important;font-size:11px!important}.address-form button{padding:3px}.table-heading,.file-row{grid-template-columns:16px minmax(125px,1fr) 65px;gap:9px;padding-inline:12px}.file-date,.table-heading>span:nth-child(3){display:none}.file-name{gap:8px}.name-lines strong{font-size:11px}.file-icon{width:27px;height:30px}.file-size{font-size:10px}.list-caption{padding-inline:12px;height:40px}.virtual-list{height:48vh;min-height:260px}.selection-bar{left:50%;bottom:76px;max-width:calc(100vw - 20px);gap:2px;padding:8px 9px;box-shadow:0 8px 30px #233c3238}.selection-bar button{font-size:9px;padding:5px 7px;gap:4px;flex-direction:column}.selection-bar button i{font-size:18px}.selection-bar strong{font-size:10px;margin:0 3px}.selection-divider{margin-inline:3px}.selection-bar .clear-selection{align-self:center}.browser-layout.with-side{display:block}.detail-panel{width:100%;margin-top:18px}.with-side .table-heading,.with-side .file-row{grid-template-columns:16px minmax(125px,1fr) 65px}.with-side .file-size,.with-side .table-heading>span:nth-child(4){display:block}.preview-image{max-height:280px}.activity-list,.trash-list{padding:18px 14px}.section-heading h2{font-size:15px}.section-note{font-size:11px}.job-card{padding:15px}.trash-row{gap:9px;flex-wrap:wrap}.trash-row>span:not(.file-icon){display:none}.index-status{align-items:flex-start;flex-wrap:wrap;font-size:10px}.index-status>span{flex:1}.index-status button{margin-left:24px}.duplicates-panel{padding:16px}.duplicate-summary{gap:7px;font-size:10px;flex-wrap:wrap}.duplicate-summary button{font-size:9px}.duplicate-file .keeper-label{font-size:8px}}
@media(prefers-reduced-motion:reduce){.file-row{transition:none}.loading-ring{animation:none}}
</style>

<style scoped>
.browser-tools {
  display: grid !important;
  grid-template-columns: minmax(220px, 1fr) auto auto;
  align-items: center;
  gap: 0.65rem;
  min-width: 0;
}

.browser-tools .filter-input {
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
  margin: 0;
  padding-inline: 0.9rem;
  border: 1px solid var(--surfaceSecondary);
  border-radius: 9px;
  background: var(--background);
  transition: border-color 120ms ease, box-shadow 120ms ease;
}

.browser-tools .filter-input:focus-within {
  border-color: var(--primaryColor);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--primaryColor) 15%, transparent);
}

.browser-tools .filter-input input {
  min-width: 0;
  height: 2.8rem;
  font-size: 0.875rem;
}

.browser-tools .filter-input kbd {
  flex: 0 0 auto;
  min-width: 1.35rem;
  padding: 0.15rem 0.35rem;
  border: 1px solid var(--surfaceSecondary);
  border-radius: 4px;
  background: #f8faf6;
  text-align: center;
}

.browser-filter-controls,
.browser-view-controls {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  min-width: 0;
}

.browser-filter-controls select,
.browser-view-controls select {
  min-width: 0;
  max-width: 11rem;
  height: 2.8rem;
  border: 1px solid var(--surfaceSecondary);
  border-radius: 8px;
  background: var(--background);
}

.browser-filter-controls button,
.browser-view-controls button {
  min-width: 2.8rem;
  min-height: 2.8rem;
  border: 1px solid var(--surfaceSecondary);
  border-radius: 8px;
  background: var(--background);
}

.browser-filter-controls .filter-toggle {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  white-space: nowrap;
}

.browser-filter-controls .filter-toggle.active,
.browser-filter-controls .filter-toggle:has(.filter-count) {
  border-color: color-mix(in srgb, var(--primaryColor) 45%, var(--surfaceSecondary));
  color: var(--primaryColor);
}

.filter-count {
  display: inline-grid;
  min-width: 1.25rem;
  height: 1.25rem;
  padding-inline: 0.15rem;
  place-items: center;
  border-radius: 999px;
  background: color-mix(in srgb, var(--primaryColor) 12%, white);
  font-size: 0.72rem;
  font-variant-numeric: tabular-nums;
}

.index-status {
  align-items: flex-start;
  color: var(--textSecondary);
}

.index-details {
  display: flex;
  flex: 1 1 auto;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 0.25rem 0.75rem;
  min-width: 0;
}

.scan-scope {
  color: var(--textPrimary);
  font-weight: 550;
  overflow-wrap: anywhere;
}

.scan-state {
  color: var(--primaryColor);
  font-size: 0.72rem;
  font-weight: 600;
  text-transform: capitalize;
}

.scan-state--failed,
.scan-state--error {
  color: #ad543f;
}

.scan-error {
  flex-basis: 100%;
  overflow-wrap: anywhere;
}

.duplicate-toggle {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 0.3rem;
  min-height: 2.5rem;
  padding: 0.4rem 0.7rem;
  border: 1px solid var(--surfaceSecondary);
  border-radius: 8px;
  background: var(--background);
  color: var(--primaryColor);
}

.duplicates-panel {
  border-color: color-mix(in srgb, var(--primaryColor) 18%, var(--surfaceSecondary));
  box-shadow: 0 8px 24px rgb(35 59 53 / 5%);
}

.duplicate-group {
  padding: 0.9rem 0.55rem;
  border-radius: 8px;
}

.duplicate-group + .duplicate-group {
  margin-top: 0.25rem;
}

.duplicate-summary {
  flex-wrap: wrap;
}

.duplicate-summary .select-other-copies {
  min-height: 2.4rem;
  padding: 0.45rem 0.75rem;
  border-radius: 7px;
  color: var(--primaryColor);
  white-space: nowrap;
}

.duplicate-file {
  min-width: 0;
  padding: 0.6rem 0.55rem;
  border-radius: 6px;
  color: var(--textSecondary);
  transition: background-color 120ms ease;
}

.duplicate-file.is-keeper {
  background: color-mix(in srgb, var(--primaryColor) 9%, white);
  color: var(--textPrimary);
}

.duplicate-path {
  min-width: 0;
  overflow-wrap: anywhere;
}

.keeper-label {
  min-width: 3.6rem;
}

@media (width <= 1050px) {
  .browser-tools {
    grid-template-columns: minmax(0, 1fr) auto;
  }

  .browser-tools .filter-input {
    grid-column: 1 / -1;
  }

  .browser-filter-controls {
    justify-self: start;
  }

  .browser-view-controls {
    justify-self: end;
  }
}

@media (width <= 680px) {
  .browser-tools {
    grid-template-columns: minmax(0, 1fr);
    gap: 0.55rem;
  }

  .browser-tools .filter-input {
    grid-column: auto;
  }

  .browser-filter-controls,
  .browser-view-controls {
    width: 100%;
    justify-self: stretch;
    flex-wrap: wrap;
  }

  .browser-filter-controls select,
  .browser-view-controls select {
    flex: 1 1 7.5rem;
    max-width: none;
  }

  .browser-view-controls {
    justify-content: flex-start;
  }

  .index-status {
    flex-wrap: wrap;
  }

  .index-details {
    flex-basis: calc(100% - 2rem);
  }

  .duplicate-toggle {
    margin-left: 1.6rem;
  }

  .duplicate-summary .select-other-copies {
    margin-left: 0;
  }

  .duplicate-file {
    align-items: flex-start;
  }
}

@media (prefers-reduced-motion: reduce) {
  .browser-tools .filter-input {
    transition: none;
  }
}
</style>
