export namespace main {
	
	export class ChipDTO {
	    label: string;
	    field: string;
	    op: string;
	    value: string;
	    clauseKey: string;
	
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
	    }
	}
	export class ParseQueryResult {
	    chips: ChipDTO[];
	    semanticQuery: string;
	    hasFilters: boolean;
	    cacheHit: boolean;
	    isOffline: boolean;
	
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
	    }
	}
	export class SearchWithFiltersResult {
	    results: SearchResultDTO[];
	    relaxationBanner?: string;
	
	    static createFrom(source: any = {}) {
	        return new SearchWithFiltersResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.results = this.convertValues(source["results"], SearchResultDTO);
	        this.relaxationBanner = source["relaxationBanner"];
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

