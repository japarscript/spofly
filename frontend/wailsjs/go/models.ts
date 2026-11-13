export namespace main {
	
	export class WindowSize {
	    width: number;
	    height: number;
	
	    static createFrom(source: any = {}) {
	        return new WindowSize(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.width = source["width"];
	        this.height = source["height"];
	    }
	}
	export class Settings {
	    mode: string;
	    theme: string;
	    fontScale: number;
	    opacity: number;
	    showAlbumArt: boolean;
	    showNextInPill: boolean;
	    globalOffsetMs: number;
	    trackOffsets?: Record<string, number>;
	    modeSizes?: Record<string, WindowSize>;
	    locked: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.theme = source["theme"];
	        this.fontScale = source["fontScale"];
	        this.opacity = source["opacity"];
	        this.showAlbumArt = source["showAlbumArt"];
	        this.showNextInPill = source["showNextInPill"];
	        this.globalOffsetMs = source["globalOffsetMs"];
	        this.trackOffsets = source["trackOffsets"];
	        this.modeSizes = this.convertValues(source["modeSizes"], WindowSize, true);
	        this.locked = source["locked"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LyricWord {
	    text: string;
	    startMs: number;
	    endMs: number;
	
	    static createFrom(source: any = {}) {
	        return new LyricWord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.startMs = source["startMs"];
	        this.endMs = source["endMs"];
	    }
	}
	export class LyricLine {
	    startMs: number;
	    endMs: number;
	    text: string;
	    words?: LyricWord[];
	
	    static createFrom(source: any = {}) {
	        return new LyricLine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.startMs = source["startMs"];
	        this.endMs = source["endMs"];
	        this.text = source["text"];
	        this.words = this.convertValues(source["words"], LyricWord);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LyricsPayload {
	    trackKey: string;
	    trackName: string;
	    artistName: string;
	    source: string;
	    synced: boolean;
	    wordSynced: boolean;
	    instrumental: boolean;
	    lines: LyricLine[];
	    plainText: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new LyricsPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trackKey = source["trackKey"];
	        this.trackName = source["trackName"];
	        this.artistName = source["artistName"];
	        this.source = source["source"];
	        this.synced = source["synced"];
	        this.wordSynced = source["wordSynced"];
	        this.instrumental = source["instrumental"];
	        this.lines = this.convertValues(source["lines"], LyricLine);
	        this.plainText = source["plainText"];
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TrackState {
	    running: boolean;
	    playing: boolean;
	    trackId: string;
	    trackKey: string;
	    title: string;
	    artist: string;
	    album: string;
	    durationMs: number;
	    positionMs: number;
	    artworkUrl: string;
	    updatedAtMs: number;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new TrackState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.playing = source["playing"];
	        this.trackId = source["trackId"];
	        this.trackKey = source["trackKey"];
	        this.title = source["title"];
	        this.artist = source["artist"];
	        this.album = source["album"];
	        this.durationMs = source["durationMs"];
	        this.positionMs = source["positionMs"];
	        this.artworkUrl = source["artworkUrl"];
	        this.updatedAtMs = source["updatedAtMs"];
	        this.source = source["source"];
	    }
	}
	export class AppSnapshot {
	    track: TrackState;
	    lyrics?: LyricsPayload;
	    settings: Settings;
	    platform: string;
	
	    static createFrom(source: any = {}) {
	        return new AppSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.track = this.convertValues(source["track"], TrackState);
	        this.lyrics = this.convertValues(source["lyrics"], LyricsPayload);
	        this.settings = this.convertValues(source["settings"], Settings);
	        this.platform = source["platform"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	export class LyricsSearchResult {
	    id: number;
	    trackName: string;
	    artistName: string;
	    albumName: string;
	    duration: number;
	    wordSynced: boolean;
	    synced: boolean;
	    instrumental: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LyricsSearchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.trackName = source["trackName"];
	        this.artistName = source["artistName"];
	        this.albumName = source["albumName"];
	        this.duration = source["duration"];
	        this.wordSynced = source["wordSynced"];
	        this.synced = source["synced"];
	        this.instrumental = source["instrumental"];
	    }
	}
	
	

}

