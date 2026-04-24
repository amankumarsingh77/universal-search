export namespace app {
	
	export class ChipDTO {
	    label: string;
	    field: string;
	    op: string;
	    value: string;
	    clauseKey: string;
	    clauseType: string;
	
	    static createFrom(source: any = {}) {
	        return new ChipDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.field = source["field"];
	        this.op = source["op"];
	        this.value = source["value"];
	        this.clauseKey = source["clauseKey"];
	        this.clauseType = source["clauseType"];
	    }
	}
	export class FailureGroupDTO {
	    code: string;
	    label: string;
	    count: number;
	    sampleFiles: string[];
	
	    static createFrom(source: any = {}) {
	        return new FailureGroupDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.label = source["label"];
	        this.count = source["count"];
	        this.sampleFiles = source["sampleFiles"];
	    }
	}
	export class IndexFailureDTO {
	    path: string;
	    code: string;
	    message: string;
	    attempts: number;
	    lastFailedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new IndexFailureDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.code = source["code"];
	        this.message = source["message"];
	        this.attempts = source["attempts"];
	        this.lastFailedAt = source["lastFailedAt"];
	    }
	}
	export class IndexStatusDTO {
	    totalFiles: number;
	    indexedFiles: number;
	    failedFiles: number;
	    currentFile: string;
	    isRunning: boolean;
	    paused: boolean;
	    quotaPaused: boolean;
	    quotaResumeAt: string;
	    pendingRetryFiles: number;
	    failedFileGroups: FailureGroupDTO[];
	
	    static createFrom(source: any = {}) {
	        return new IndexStatusDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.totalFiles = source["totalFiles"];
	        this.indexedFiles = source["indexedFiles"];
	        this.failedFiles = source["failedFiles"];
	        this.currentFile = source["currentFile"];
	        this.isRunning = source["isRunning"];
	        this.paused = source["paused"];
	        this.quotaPaused = source["quotaPaused"];
	        this.quotaResumeAt = source["quotaResumeAt"];
	        this.pendingRetryFiles = source["pendingRetryFiles"];
	        this.failedFileGroups = this.convertValues(source["failedFileGroups"], FailureGroupDTO);
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
	export class ParseQueryResult {
	    chips: ChipDTO[];
	    semanticQuery: string;
	    hasFilters: boolean;
	    cacheHit: boolean;
	    isOffline: boolean;
	    errorCode?: string;
	    warning?: string;
	    retryAfterMs?: number;
	
	    static createFrom(source: any = {}) {
	        return new ParseQueryResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.chips = this.convertValues(source["chips"], ChipDTO);
	        this.semanticQuery = source["semanticQuery"];
	        this.hasFilters = source["hasFilters"];
	        this.cacheHit = source["cacheHit"];
	        this.isOffline = source["isOffline"];
	        this.errorCode = source["errorCode"];
	        this.warning = source["warning"];
	        this.retryAfterMs = source["retryAfterMs"];
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
	export class SearchResultDTO {
	    filePath: string;
	    fileName: string;
	    fileType: string;
	    extension: string;
	    sizeBytes: number;
	    thumbnailPath: string;
	    startTime: number;
	    endTime: number;
	    score: number;
	    modifiedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new SearchResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filePath = source["filePath"];
	        this.fileName = source["fileName"];
	        this.fileType = source["fileType"];
	        this.extension = source["extension"];
	        this.sizeBytes = source["sizeBytes"];
	        this.thumbnailPath = source["thumbnailPath"];
	        this.startTime = source["startTime"];
	        this.endTime = source["endTime"];
	        this.score = source["score"];
	        this.modifiedAt = source["modifiedAt"];
	    }
	}
	export class SearchWithFiltersResult {
	    results: SearchResultDTO[];
	    relaxationBanner?: string;
	    errorCode?: string;
	    retryAfterMs?: number;
	
	    static createFrom(source: any = {}) {
	        return new SearchWithFiltersResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.results = this.convertValues(source["results"], SearchResultDTO);
	        this.relaxationBanner = source["relaxationBanner"];
	        this.errorCode = source["errorCode"];
	        this.retryAfterMs = source["retryAfterMs"];
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

}

